package sync

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct{ db *pgxpool.Pool }

func NewHandler(db *pgxpool.Pool) *Handler { return &Handler{db: db} }

type progressInput struct {
	Payload     json.RawMessage `json:"payload"`
	BaseVersion int64           `json:"baseVersion"`
}
type eventInput struct {
	ID              string          `json:"id"`
	DeviceID        string          `json:"deviceId"`
	EventType       string          `json:"eventType" binding:"required"`
	Payload         json.RawMessage `json:"payload"`
	ClientCreatedAt time.Time       `json:"clientCreatedAt"`
	DeletedAt       *time.Time      `json:"deletedAt"`
	FieldTimestamps json.RawMessage `json:"fieldTimestamps"`
}
type deviceInput struct {
	ID       string `json:"id"`
	Platform string `json:"platform" binding:"required"`
	Name     string `json:"name"`
}

func (h *Handler) Acknowledge(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	var in struct {
		DeviceID string    `json:"deviceId"`
		CursorAt time.Time `json:"cursorAt"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid cursor"})
		return
	}
	did, e := uuid.Parse(in.DeviceID)
	if e != nil || in.CursorAt.IsZero() {
		c.JSON(400, gin.H{"error": "deviceId and cursorAt are required"})
		return
	}
	_, e = h.db.Exec(c, `INSERT INTO sync_cursors(user_id,device_id,cursor_at) VALUES($1,$2,$3) ON CONFLICT(user_id,device_id) DO UPDATE SET cursor_at=GREATEST(sync_cursors.cursor_at,EXCLUDED.cursor_at),updated_at=NOW()`, uid, did, in.CursorAt)
	if e != nil {
		c.JSON(500, gin.H{"error": "cursor update failed"})
		return
	}
	c.Status(204)
}

func (h *Handler) RegisterDevice(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	var in deviceInput
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid device"})
		return
	}
	id, e := uuid.Parse(in.ID)
	if e != nil {
		id = uuid.New()
	}
	_, e = h.db.Exec(c, `INSERT INTO devices(id,user_id,platform,name) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET user_id=$2,platform=$3,name=$4,last_seen_at=NOW()`, id, uid, in.Platform, in.Name)
	if e != nil {
		c.JSON(500, gin.H{"error": "device registration failed"})
		return
	}
	c.JSON(200, gin.H{"deviceId": id})
}

func (h *Handler) Events(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	after := c.Query("after")
	var rows pgx.Rows
	if after == "" {
		rows, err = h.db.Query(c, `SELECT id,device_id,event_type,payload,client_created_at,created_at,deleted_at,field_timestamps FROM sync_events WHERE user_id=$1 AND archived_at IS NULL ORDER BY created_at LIMIT 500`, uid)
	} else {
		rows, err = h.db.Query(c, `SELECT id,device_id,event_type,payload,client_created_at,created_at,deleted_at,field_timestamps FROM sync_events WHERE user_id=$1 AND archived_at IS NULL AND created_at>$2::timestamptz ORDER BY created_at LIMIT 500`, uid, after)
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var id uuid.UUID
		var device *uuid.UUID
		var typ string
		var payload []byte
		var client, created time.Time
		var deleted *time.Time
		var fieldTimestamps []byte
		if err := rows.Scan(&id, &device, &typ, &payload, &client, &created, &deleted, &fieldTimestamps); err != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "deviceId": device, "eventType": typ, "payload": json.RawMessage(payload), "clientCreatedAt": client, "createdAt": created, "deletedAt": deleted, "fieldTimestamps": json.RawMessage(fieldTimestamps)})
	}
	c.JSON(200, gin.H{"events": out})
}

func (h *Handler) ArchiveEvents(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	before := c.Query("before")
	if before == "" {
		c.JSON(400, gin.H{"error": "before is required"})
		return
	}
	tag, e := h.db.Exec(c, `UPDATE sync_events SET archived_at=NOW() WHERE user_id=$1 AND created_at<$2::timestamptz AND archived_at IS NULL`, uid, before)
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid archive time"})
		return
	}
	c.JSON(200, gin.H{"archived": tag.RowsAffected()})
}

func (h *Handler) CompactEvents(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	before := c.Query("before")
	if before == "" {
		c.JSON(400, gin.H{"error": "before is required"})
		return
	}
	rows, err := h.db.Query(c, `SELECT id,event_type,payload,client_created_at,deleted_at FROM sync_events WHERE user_id=$1 AND created_at<$2::timestamptz AND archived_at IS NULL ORDER BY created_at LIMIT 5000`, uid, before)
	if err != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	type compacted struct {
		ID              uuid.UUID       `json:"id"`
		EventType       string          `json:"eventType"`
		Payload         json.RawMessage `json:"payload"`
		ClientCreatedAt time.Time       `json:"clientCreatedAt"`
		DeletedAt       *time.Time      `json:"deletedAt,omitempty"`
	}
	items := make([]compacted, 0)
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var item compacted
		if rows.Scan(&item.ID, &item.EventType, &item.Payload, &item.ClientCreatedAt, &item.DeletedAt) == nil {
			items = append(items, item)
			ids = append(ids, item.ID)
		}
	}
	if len(items) == 0 {
		c.JSON(200, gin.H{"compressed": 0})
		return
	}
	payload, _ := json.Marshal(gin.H{"kind": "sync_compaction", "from": ids[0], "to": ids[len(ids)-1], "events": items})
	compactID := uuid.New()
	if _, err = h.db.Exec(c, `INSERT INTO sync_events(id,user_id,event_type,payload,client_created_at) VALUES($1,$2,'sync.compaction',$3,NOW())`, compactID, uid, payload); err != nil {
		c.JSON(500, gin.H{"error": "compaction failed"})
		return
	}
	if _, err = h.db.Exec(c, `UPDATE sync_events SET archived_at=NOW() WHERE user_id=$1 AND created_at<$2::timestamptz AND archived_at IS NULL AND id<>$3`, uid, before, compactID); err != nil {
		c.JSON(500, gin.H{"error": "archive failed"})
		return
	}
	c.JSON(200, gin.H{"compressed": len(items), "eventId": compactID, "replayable": true})
}

func (h *Handler) ReplayEvents(c *gin.Context) {
	uid, e := uuid.Parse(c.GetString("userID"))
	if e != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
	from := c.Query("from")
	if from == "" {
		c.JSON(400, gin.H{"error": "from is required"})
		return
	}
	rows, e := h.db.Query(c, `SELECT id,device_id,event_type,payload,client_created_at,created_at,deleted_at,field_timestamps FROM sync_events WHERE user_id=$1 AND created_at>=$2::timestamptz ORDER BY created_at LIMIT 5000`, uid, from)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id uuid.UUID
		var device *uuid.UUID
		var typ string
		var payload []byte
		var client, created time.Time
		var deleted *time.Time
		var fieldTimestamps []byte
		if e := rows.Scan(&id, &device, &typ, &payload, &client, &created, &deleted, &fieldTimestamps); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "deviceId": device, "eventType": typ, "payload": json.RawMessage(payload), "clientCreatedAt": client, "createdAt": created, "deletedAt": deleted, "fieldTimestamps": json.RawMessage(fieldTimestamps)})
	}
	c.JSON(200, gin.H{"events": out, "replay": true})
}

func (h *Handler) GetProgress(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user"})
		return
	}
	var payload []byte
	var version int64
	err = h.db.QueryRow(c, `SELECT payload,version FROM learning_progress WHERE user_id=$1`, uid).Scan(&payload, &version)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"payload": json.RawMessage(`{}`), "version": int64(0)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"payload": json.RawMessage(payload), "version": version})
}

func (h *Handler) PutProgress(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user"})
		return
	}
	var in progressInput
	if c.ShouldBindJSON(&in) != nil || len(in.Payload) == 0 || !json.Valid(in.Payload) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload must be valid JSON"})
		return
	}
	var version int64
	err = h.db.QueryRow(c, `INSERT INTO learning_progress(user_id,payload,version) VALUES($1,$3,1) ON CONFLICT(user_id) DO UPDATE SET payload=EXCLUDED.payload,version=learning_progress.version+1,updated_at=NOW() WHERE $2=0 OR learning_progress.version=$2 RETURNING version`, uid, in.BaseVersion, in.Payload).Scan(&version)
	if err != nil {
		var current []byte
		var currentVersion int64
		if qerr := h.db.QueryRow(c, `SELECT payload,version FROM learning_progress WHERE user_id=$1`, uid).Scan(&current, &currentVersion); qerr != nil {
			current = []byte(`{}`)
			currentVersion = 0
		}
		c.JSON(http.StatusConflict, gin.H{"error": "progress version conflict", "code": "SYNC_CONFLICT", "current": gin.H{"payload": json.RawMessage(current), "version": currentVersion}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"version": version, "updatedAt": time.Now().UTC()})
}

func (h *Handler) AppendEvents(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user"})
		return
	}
	var events []eventInput
	if c.ShouldBindJSON(&events) != nil || len(events) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid events"})
		return
	}
	accepted := 0
	failed := make([]string, 0)
	for _, e := range events {
		if e.EventType == "" || len(e.Payload) == 0 || !json.Valid(e.Payload) {
			failed = append(failed, e.ID)
			continue
		}
		id, er := uuid.Parse(e.ID)
		if er != nil {
			id = uuid.New()
		}
		var device any
		if d, er := uuid.Parse(e.DeviceID); er == nil {
			device = d
		}
		created := e.ClientCreatedAt
		if created.IsZero() {
			created = time.Now().UTC()
		}
		fieldTimestamps := e.FieldTimestamps
		if len(fieldTimestamps) == 0 || !json.Valid(fieldTimestamps) {
			fieldTimestamps = json.RawMessage(`{}`)
		}
		_, er = h.db.Exec(c, `INSERT INTO sync_events(id,user_id,device_id,event_type,payload,client_created_at,deleted_at,field_timestamps) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, id, uid, device, e.EventType, e.Payload, created, e.DeletedAt, fieldTimestamps)
		if er == nil {
			accepted++
		} else {
			failed = append(failed, e.ID)
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": accepted, "received": len(events), "failed": failed, "retryable": len(failed) > 0})
}
