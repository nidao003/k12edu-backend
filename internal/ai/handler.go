package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	baseURL, apiKey string
	client          *http.Client
	db              *pgxpool.Pool
	redis           *redis.Client
	mu              sync.Mutex
	recent          map[string][]time.Time
}

func NewHandler(baseURL, apiKey string, pool *pgxpool.Pool, rdb ...*redis.Client) *Handler {
	var client *redis.Client
	if len(rdb) > 0 {
		client = rdb[0]
	}
	return &Handler{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{Timeout: 90 * time.Second}, db: pool, redis: client, recent: map[string][]time.Time{}}
}

func (h *Handler) allowed(user string) bool {
	if h.redis != nil {
		key := "k12edu:ai:rate:" + user
		n, err := h.redis.Incr(context.Background(), key).Result()
		if err == nil {
			if n == 1 {
				h.redis.Expire(context.Background(), key, time.Minute)
			}
			return n <= 20
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	old := h.recent[user]
	keep := old[:0]
	for _, t := range old {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 20 {
		h.recent[user] = keep
		return false
	}
	h.recent[user] = append(keep, now)
	return true
}
func (h *Handler) Chat(c *gin.Context) {
	if !h.allowed(c.GetString("userID")) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "AI request rate limit exceeded"})
		return
	}
	if h.baseURL == "" || h.apiKey == "" {
		c.JSON(503, gin.H{"error": "AI provider is not configured"})
		return
	}
	body, e := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if e != nil || !json.Valid(body) {
		c.JSON(400, gin.H{"error": "invalid JSON"})
		return
	}
	req, e := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, h.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if e != nil {
		c.JSON(500, gin.H{"error": "create request failed"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	resp, e := h.client.Do(req)
	if e != nil {
		c.JSON(502, gin.H{"error": "AI provider unavailable"})
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if h.db != nil {
		uid, _ := uuid.Parse(c.GetString("userID"))
		var in struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &in)
		_, _ = h.db.Exec(c, `INSERT INTO ai_usage(id,user_id,model,provider_status,input_bytes,output_bytes) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), uid, in.Model, resp.StatusCode, len(body), len(data))
	}
	c.Data(resp.StatusCode, "application/json", data)
}
