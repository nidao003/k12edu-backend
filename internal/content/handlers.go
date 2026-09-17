package content

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"net/http"
	"time"
)

type Handler struct {
	db    *pgxpool.Pool
	cache *redis.Client
}

func NewHandler(db *pgxpool.Pool, cache ...*redis.Client) *Handler {
	var rdb *redis.Client
	if len(cache) > 0 {
		rdb = cache[0]
	}
	return &Handler{db: db, cache: rdb}
}

type input struct {
	Kind       string          `json:"kind" binding:"required"`
	ExternalID string          `json:"externalId"`
	Title      string          `json:"title" binding:"required"`
	Payload    json.RawMessage `json:"payload"`
	Published  bool            `json:"published"`
}

func (h *Handler) List(c *gin.Context) {
	kind := c.Query("kind")
	rows, e := h.db.Query(c, `SELECT id,kind,external_id,title,payload,published,updated_at FROM content_items WHERE ($1='' OR kind=$1) ORDER BY updated_at DESC LIMIT 200`, kind)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var id uuid.UUID
		var k, eid, title string
		var payload []byte
		var pub bool
		var updated any
		if e := rows.Scan(&id, &k, &eid, &title, &payload, &pub, &updated); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "kind": k, "externalId": eid, "title": title, "payload": json.RawMessage(payload), "published": pub, "updatedAt": updated})
	}
	c.JSON(200, gin.H{"items": out})
}

func (h *Handler) Published(c *gin.Context) {
	kind := c.Query("kind")
	key := "k12edu:content:published:" + kind
	if h.cache != nil {
		if raw, err := h.cache.Get(c, key).Bytes(); err == nil {
			c.Data(200, "application/json", raw)
			return
		}
	}
	rows, err := h.db.Query(c, `SELECT id,kind,external_id,title,payload,published,updated_at FROM content_items WHERE published=true AND ($1='' OR kind=$1) ORDER BY updated_at DESC LIMIT 500`, kind)
	if err != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var id uuid.UUID
		var k, eid, title string
		var payload []byte
		var pub bool
		var updated any
		if err := rows.Scan(&id, &k, &eid, &title, &payload, &pub, &updated); err != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "kind": k, "externalId": eid, "title": title, "payload": json.RawMessage(payload), "published": pub, "updatedAt": updated})
	}
	response, _ := json.Marshal(gin.H{"items": out})
	if h.cache != nil {
		_ = h.cache.Set(c, key, response, 5*time.Minute).Err()
	}
	c.Data(200, "application/json", response)
}

func (h *Handler) InvalidateCache(kind string) {
	if h.cache != nil {
		_ = h.cache.Del(context.Background(), "k12edu:content:published:"+kind).Err()
	}
}
func (h *Handler) InvalidateAll() {
	if h.cache == nil {
		return
	}
	ctx := context.Background()
	iter := h.cache.Scan(ctx, 0, "k12edu:content:published:*", 0).Iterator()
	for iter.Next(ctx) {
		_ = h.cache.Del(ctx, iter.Val()).Err()
	}
}
func (h *Handler) Create(c *gin.Context) {
	var in input
	if c.ShouldBindJSON(&in) != nil || len(in.Payload) == 0 || !json.Valid(in.Payload) {
		c.JSON(400, gin.H{"error": "invalid content"})
		return
	}
	id := uuid.New()
	_, e := h.db.Exec(c, `INSERT INTO content_items(id,kind,external_id,title,payload,published) VALUES($1,$2,$3,$4,$5,$6)`, id, in.Kind, in.ExternalID, in.Title, in.Payload, in.Published)
	if e != nil {
		c.JSON(409, gin.H{"error": "content already exists or cannot be saved"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
	h.InvalidateAll()
}
func (h *Handler) Update(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	var in input
	if c.ShouldBindJSON(&in) != nil || len(in.Payload) == 0 || !json.Valid(in.Payload) {
		c.JSON(400, gin.H{"error": "invalid content"})
		return
	}
	tag, e := h.db.Exec(c, `UPDATE content_items SET kind=$2,external_id=$3,title=$4,payload=$5,published=$6,updated_at=NOW() WHERE id=$1`, id, in.Kind, in.ExternalID, in.Title, in.Payload, in.Published)
	if e != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "content not found"})
		return
	}
	c.Status(204)
	h.InvalidateAll()
}
func (h *Handler) Delete(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	tag, e := h.db.Exec(c, `DELETE FROM content_items WHERE id=$1`, id)
	if e != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "content not found"})
		return
	}
	c.Status(204)
	h.InvalidateAll()
}
