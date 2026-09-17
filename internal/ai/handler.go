package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	monthlyCostLimit      int
	providerFailures      int
	providerOpenUntil     time.Time
}

func (h *Handler) Health(c *gin.Context) {
	h.mu.Lock()
	open := time.Now().Before(h.providerOpenUntil)
	failures := h.providerFailures
	h.mu.Unlock()
	if h.baseURL == "" || h.apiKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unconfigured"})
		return
	}
	if open {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "circuit_open", "failures": failures})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "failures": failures})
}

func (h *Handler) AppealSafety(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("userID"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event"})
		return
	}
	tag, err := h.db.Exec(c, `UPDATE ai_safety_events SET appeal_status='pending' WHERE id=$1 AND user_id=$2 AND status IN ('rejected','escalated')`, id, uid)
	if err != nil || tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "appeal is not available"})
		return
	}
	c.Status(http.StatusAccepted)
}

func NewHandler(baseURL, apiKey string, pool *pgxpool.Pool, quota, inputCost, outputCost, monthlyCostLimit int, rdb ...*redis.Client) *Handler {
	var client *redis.Client
	if len(rdb) > 0 {
		client = rdb[0]
	}
	return &Handler{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{Timeout: 90 * time.Second}, db: pool, redis: client, recent: map[string][]time.Time{}, monthlyQuota: quota, inputCost: inputCost, outputCost: outputCost, monthlyCostLimit: monthlyCostLimit}
}

func (h *Handler) withinCostLimit(c *gin.Context, uid uuid.UUID) bool {
	if h.db == nil || h.monthlyCostLimit <= 0 {
		return true
	}
	var spent int64
	if err := h.db.QueryRow(c, `SELECT COALESCE(cost_micros,0) FROM ai_policies WHERE user_id=$1 AND period=$2`, uid, time.Now().UTC().Format("2006-01")).Scan(&spent); err != nil {
		return true
	}
	return spent < int64(h.monthlyCostLimit)
}

func (h *Handler) withinBalance(c *gin.Context, uid uuid.UUID, bodySize int) bool {
	if h.db == nil {
		return true
	}
	var balance int64
	if err := h.db.QueryRow(c, `SELECT balance_micros FROM ai_wallets WHERE user_id=$1`, uid).Scan(&balance); err != nil {
		return true
	}
	minimum := int64((bodySize / 4) * h.inputCost / 1000)
	return balance >= minimum
}

func (h *Handler) admit(c *gin.Context, uid uuid.UUID) bool {
	if h.db == nil || h.monthlyQuota <= 0 {
		return true
	}
	quota := h.monthlyQuota
	_ = h.db.QueryRow(c, `SELECT COALESCE(p.monthly_requests,$2) FROM users u LEFT JOIN ai_plans p ON p.id=u.ai_plan_id WHERE u.id=$1`, uid, h.monthlyQuota).Scan(&quota)
	month := time.Now().UTC().Format("2006-01")
	var n int
	err := h.db.QueryRow(c, `INSERT INTO ai_policies(id,user_id,period,request_count) VALUES($1,$2,$3,1) ON CONFLICT(user_id,period) DO UPDATE SET request_count=ai_policies.request_count+1,updated_at=NOW() WHERE ai_policies.request_count < $4 RETURNING request_count`, uuid.New(), uid, month, quota).Scan(&n)
	return err == nil
}
func (h *Handler) safety(c *gin.Context, uid uuid.UUID, body []byte) bool {
	lower := strings.ToLower(string(body))
	terms := []string{"自杀", "自残", "杀人", "色情", "炸弹", "信用卡号", "password", "ignore previous instructions", "忽略之前的指令", "reveal system prompt", "system prompt"}
	var minor bool
	if h.db != nil {
		_ = h.db.QueryRow(c, `SELECT minor_mode FROM users WHERE id=$1`, uid).Scan(&minor)
	}
	if minor {
		terms = append(terms, "裸聊", "成人视频", "博彩", "毒品", "约炮", "色情小说")
	}
	for _, term := range terms {
		if strings.Contains(lower, term) {
			sum := sha256.Sum256(body)
			if h.db != nil {
				severity := "high"
				if minor {
					severity = "critical"
				}
				if strings.Contains(lower, "password") || strings.Contains(lower, "system prompt") {
					severity = "critical"
				}
				_, _ = h.db.Exec(c, `INSERT INTO ai_safety_events(id,user_id,reason,content_hash,severity) VALUES($1,$2,$3,$4,$5)`, uuid.New(), uid, "policy_or_injection", fmt.Sprintf("%x", sum[:]), severity)
			}
			return false
		}
	}
	return true
}

var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`),
	regexp.MustCompile(`\b(?:\+?86[- ]?)?1[3-9]\d{9}\b`),
	regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
}

func redactPII(body []byte) []byte {
	var walk func(any) any
	walk = func(value any) any {
		switch v := value.(type) {
		case string:
			for _, pattern := range piiPatterns {
				v = pattern.ReplaceAllString(v, "[REDACTED]")
			}
			return v
		case []any:
			for i := range v {
				v[i] = walk(v[i])
			}
			return v
		case map[string]any:
			for k := range v {
				v[k] = walk(v[k])
			}
			return v
		default:
			return value
		}
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return body
	}
	clean, err := json.Marshal(walk(value))
	if err != nil {
		return body
	}
	return clean
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
	h.mu.Lock()
	if time.Now().Before(h.providerOpenUntil) {
		h.mu.Unlock()
		c.JSON(503, gin.H{"code": "AI_PROVIDER_CIRCUIT_OPEN", "error": "AI provider temporarily unavailable"})
		return
	}
	h.mu.Unlock()
	body, e := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if e != nil || !json.Valid(body) {
		c.JSON(400, gin.H{"error": "invalid JSON"})
		return
	}
	body = redactPII(body)
	if !h.withinBalance(c, uid, len(body)) {
		c.JSON(http.StatusPaymentRequired, gin.H{"code": "AI_BALANCE_INSUFFICIENT", "error": "AI wallet balance is insufficient"})
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
	if !h.withinCostLimit(c, uid) {
		c.JSON(http.StatusPaymentRequired, gin.H{"code": "AI_COST_LIMIT_EXCEEDED", "error": "monthly AI cost limit exceeded"})
		return
	}
	req, e := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, h.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if e != nil {
		c.JSON(500, gin.H{"error": "create request failed"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			var bodyErr error
			req.Body, bodyErr = req.GetBody()
			if bodyErr != nil {
				c.JSON(502, gin.H{"error": "AI request body unavailable"})
				return
			}
		}
		resp, e = h.client.Do(req)
		if e == nil && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		if attempt == 2 {
			h.mu.Lock()
			h.providerFailures++
			if h.providerFailures >= 5 {
				h.providerOpenUntil = time.Now().Add(30 * time.Second)
				h.providerFailures = 0
			}
			h.mu.Unlock()
			c.JSON(502, gin.H{"error": "AI provider unavailable"})
			return
		}
		time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
	}
	h.mu.Lock()
	h.providerFailures = 0
	h.mu.Unlock()
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if h.db != nil {
		var in struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &in)
		month := time.Now().UTC().Format("2006-01")
		inTok := len(body) / 4
		outTok := len(data) / 4
		var usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		}
		var providerPayload struct {
			Usage usage `json:"usage"`
		}
		if json.Unmarshal(data, &providerPayload) == nil {
			if providerPayload.Usage.PromptTokens > 0 {
				inTok = providerPayload.Usage.PromptTokens
			}
			if providerPayload.Usage.CompletionTokens > 0 {
				outTok = providerPayload.Usage.CompletionTokens
			}
		}
		inputCost, outputCost := h.inputCost, h.outputCost
		_ = h.db.QueryRow(c, `SELECT COALESCE(p.input_cost_micros_per_1k,$2),COALESCE(p.output_cost_micros_per_1k,$3) FROM users u LEFT JOIN ai_plans p ON p.id=u.ai_plan_id WHERE u.id=$1`, uid, h.inputCost, h.outputCost).Scan(&inputCost, &outputCost)
		cost := int64((inTok*inputCost + outTok*outputCost) / 1000)
		_, _ = h.db.Exec(c, `INSERT INTO ai_usage(id,user_id,model,provider_status,input_bytes,output_bytes,input_tokens,output_tokens,cost_micros) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.New(), uid, in.Model, resp.StatusCode, len(body), len(data), inTok, outTok, cost)
		if cost > 0 {
			_, _ = h.db.Exec(c, `INSERT INTO ai_wallets(user_id,balance_micros) VALUES($1,0) ON CONFLICT(user_id) DO NOTHING`, uid)
			_, _ = h.db.Exec(c, `UPDATE ai_wallets SET balance_micros=balance_micros-$2,updated_at=NOW() WHERE user_id=$1`, uid, cost)
			_, _ = h.db.Exec(c, `INSERT INTO ai_ledger(id,user_id,kind,amount_micros,metadata) VALUES($1,$2,'usage',$3,$4)`, uuid.New(), uid, -cost, json.RawMessage(fmt.Sprintf(`{"model":%q,"inputTokens":%d,"outputTokens":%d}`, in.Model, inTok, outTok)))
		}
		_, _ = h.db.Exec(c, `UPDATE ai_policies SET input_tokens=input_tokens+$1,output_tokens=output_tokens+$2,cost_micros=cost_micros+$3,updated_at=NOW() WHERE user_id=$4 AND period=$5`, inTok, outTok, cost, uid, month)
	}
	c.Data(resp.StatusCode, "application/json", data)
}
