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
}
type deviceInput struct {
	ID       string `json:"id"`
	Platform string `json:"platform" binding:"required"`
	Name     string `json:"name"`
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
		rows, err = h.db.Query(c, `SELECT id,device_id,event_type,payload,client_created_at,created_at FROM sync_events WHERE user_id=$1 ORDER BY created_at LIMIT 500`, uid)
	} else {
		rows, err = h.db.Query(c, `SELECT id,device_id,event_type,payload,client_created_at,created_at FROM sync_events WHERE user_id=$1 AND created_at>$2::timestamptz ORDER BY created_at LIMIT 500`, uid, after)
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
		if err := rows.Scan(&id, &device, &typ, &payload, &client, &created); err != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "deviceId": device, "eventType": typ, "payload": json.RawMessage(payload), "clientCreatedAt": client, "createdAt": created})
	}
	c.JSON(200, gin.H{"events": out})
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
		c.JSON(http.StatusConflict, gin.H{"error": "progress version conflict", "code": "SYNC_CONFLICT"})
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
	for _, e := range events {
		if e.EventType == "" || len(e.Payload) == 0 || !json.Valid(e.Payload) {
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
		_, er = h.db.Exec(c, `INSERT INTO sync_events(id,user_id,device_id,event_type,payload,client_created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING`, id, uid, device, e.EventType, e.Payload, created)
		if er == nil {
			accepted++
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": accepted, "received": len(events)})
}
