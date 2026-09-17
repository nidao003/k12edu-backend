package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	baseURL, apiKey       string
	client                *http.Client
	db                    *pgxpool.Pool
	redis                 *redis.Client
	mu                    sync.Mutex
	recent                map[string][]time.Time
	monthlyQuota          int
	inputCost, outputCost int
}

func NewHandler(baseURL, apiKey string, pool *pgxpool.Pool, quota, inputCost, outputCost int, rdb ...*redis.Client) *Handler {
	var client *redis.Client
	if len(rdb) > 0 {
		client = rdb[0]
	}
	return &Handler{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{Timeout: 90 * time.Second}, db: pool, redis: client, recent: map[string][]time.Time{}, monthlyQuota: quota, inputCost: inputCost, outputCost: outputCost}
}

func (h *Handler) admit(c *gin.Context, uid uuid.UUID) bool {
	if h.db == nil || h.monthlyQuota <= 0 {
		return true
	}
	month := time.Now().UTC().Format("2006-01")
	var n int
	err := h.db.QueryRow(c, `INSERT INTO ai_policies(id,user_id,period,request_count) VALUES($1,$2,$3,1) ON CONFLICT(user_id,period) DO UPDATE SET request_count=ai_policies.request_count+1,updated_at=NOW() WHERE ai_policies.request_count < $4 RETURNING request_count`, uuid.New(), uid, month, h.monthlyQuota).Scan(&n)
	return err == nil
}
func (h *Handler) safety(c *gin.Context, uid uuid.UUID, body []byte) bool {
	lower := strings.ToLower(string(body))
	for _, term := range []string{"自杀", "自残", "杀人", "色情", "炸弹", "信用卡号", "password"} {
		if strings.Contains(lower, term) {
			sum := sha256.Sum256(body)
			if h.db != nil {
				_, _ = h.db.Exec(c, `INSERT INTO ai_safety_events(id,user_id,reason,content_hash) VALUES($1,$2,$3,$4)`, uuid.New(), uid, "blocked_keyword", fmt.Sprintf("%x", sum[:]))
			}
			return false
		}
	}
	return true
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
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid user"})
		return
	}
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
	if !h.safety(c, uid, body) {
		c.JSON(http.StatusForbidden, gin.H{"code": "AI_SAFETY_BLOCKED", "error": "request blocked by safety review"})
		return
	}
	if !h.admit(c, uid) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "AI_QUOTA_EXCEEDED", "error": "monthly AI quota exceeded"})
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
		var in struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &in)
		_, _ = h.db.Exec(c, `INSERT INTO ai_usage(id,user_id,model,provider_status,input_bytes,output_bytes) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), uid, in.Model, resp.StatusCode, len(body), len(data))
		month := time.Now().UTC().Format("2006-01")
		inTok := len(body) / 4
		outTok := len(data) / 4
		cost := int64((inTok*h.inputCost + outTok*h.outputCost) / 1000)
		_, _ = h.db.Exec(c, `UPDATE ai_policies SET input_tokens=input_tokens+$1,output_tokens=output_tokens+$2,cost_micros=cost_micros+$3,updated_at=NOW() WHERE user_id=$4 AND period=$5`, inTok, outTok, cost, uid, month)
	}
	c.Data(resp.StatusCode, "application/json", data)
}
