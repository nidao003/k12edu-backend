package sync

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	err = h.db.QueryRow(c, `INSERT INTO learning_progress(user_id,payload,version) VALUES($1,$2,1) ON CONFLICT(user_id) DO UPDATE SET payload=EXCLUDED.payload,version=learning_progress.version+1,updated_at=NOW() RETURNING version`, uid, in.Payload).Scan(&version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save progress failed"})
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
