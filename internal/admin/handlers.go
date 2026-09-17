package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
)

type Handler struct{ db *pgxpool.Pool }

func (h *Handler) audit(c *gin.Context, action, resource string, userID any) {
	_, _ = h.db.Exec(c, `INSERT INTO audit_logs(id,user_id,action,resource,ip) VALUES($1,$2,$3,$4,$5)`, uuid.New(), userID, action, resource, c.ClientIP())
}

func NewHandler(db *pgxpool.Pool) *Handler { return &Handler{db: db} }
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
			return
		}
		c.Next()
	}
}
func (h *Handler) Stats(c *gin.Context) {
	var users int64
	var active int64
	var aiCalls int64
	if e := h.db.QueryRow(c, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&users); e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	if e := h.db.QueryRow(c, `SELECT COUNT(DISTINCT user_id) FROM sync_events WHERE created_at > NOW()-INTERVAL '24 hours'`).Scan(&active); e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	if e := h.db.QueryRow(c, `SELECT COUNT(*) FROM ai_usage WHERE created_at > NOW()-INTERVAL '24 hours'`).Scan(&aiCalls); e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	c.JSON(200, gin.H{"users": users, "activeUsers24h": active, "aiCalls24h": aiCalls})
}
func (h *Handler) Users(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	q := strings.TrimSpace(c.Query("q"))
	includeDisabled := c.Query("includeDisabled") == "true"
	where := "deleted_at IS NULL"
	if includeDisabled {
		where = "TRUE"
	}
	rows, e := h.db.Query(c, `SELECT id,COALESCE(email,''),COALESCE(display_name,''),role,created_at,deleted_at FROM users WHERE `+where+` AND ($1='' OR email ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%') ORDER BY created_at DESC LIMIT $2 OFFSET $3`, q, size, (page-1)*size)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var id, email, name, role string
		var created, disabled any
		if e := rows.Scan(&id, &email, &name, &role, &created, &disabled); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "email": email, "displayName": name, "role": role, "disabled": disabled != nil, "createdAt": created})
	}
	var total int64
	_ = h.db.QueryRow(c, `SELECT COUNT(*) FROM users WHERE `+where+` AND ($1='' OR email ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%')`, q).Scan(&total)
	c.JSON(200, gin.H{"users": out, "page": page, "pageSize": size, "total": total})
}

func (h *Handler) AIUsage(c *gin.Context) {
	rows, e := h.db.Query(c, `SELECT model,COUNT(*),COALESCE(SUM(input_bytes),0),COALESCE(SUM(output_bytes),0),COALESCE(SUM(p.cost_micros),0) FROM ai_usage u LEFT JOIN ai_policies p ON p.user_id=u.user_id AND p.period=TO_CHAR(u.created_at,'YYYY-MM') WHERE u.created_at>NOW()-INTERVAL '30 days' GROUP BY model ORDER BY COUNT(*) DESC`)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var model string
		var calls, inBytes, outBytes, cost int64
		if e := rows.Scan(&model, &calls, &inBytes, &outBytes, &cost); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"model": model, "calls": calls, "inputBytes": inBytes, "outputBytes": outBytes, "costMicros": cost})
	}
	c.JSON(200, gin.H{"items": out})
}

func (h *Handler) AISafetyEvents(c *gin.Context) {
	rows, e := h.db.Query(c, `SELECT id,user_id,reason,content_hash,status,decision,reviewed_at,created_at FROM ai_safety_events ORDER BY created_at DESC LIMIT 200`)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, uid, reason, hash, status, decision string
		var reviewed, created any
		if e := rows.Scan(&id, &uid, &reason, &hash, &status, &decision, &reviewed, &created); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "userId": uid, "reason": reason, "contentHash": hash, "status": status, "decision": decision, "reviewedAt": reviewed, "createdAt": created})
	}
	c.JSON(200, gin.H{"items": out})
}

func (h *Handler) ReviewSafety(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	reviewer, e := uuid.Parse(c.GetString("userID"))
	var in struct {
		Status   string `json:"status"`
		Decision string `json:"decision"`
	}
	if e != nil || c.ShouldBindJSON(&in) != nil || (in.Status != "approved" && in.Status != "rejected" && in.Status != "escalated") {
		c.JSON(400, gin.H{"error": "invalid review"})
		return
	}
	tag, e := h.db.Exec(c, `UPDATE ai_safety_events SET status=$2,decision=$3,reviewer_id=$4,reviewed_at=NOW() WHERE id=$1`, id, in.Status, in.Decision, reviewer)
	if e != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "event not found"})
		return
	}
	c.Status(204)
	h.audit(c, "ai.safety.review", id.String(), reviewer)
}

func (h *Handler) Plans(c *gin.Context) {
	rows, e := h.db.Query(c, `SELECT id,name,monthly_requests,input_cost_micros_per_1k,output_cost_micros_per_1k FROM ai_plans ORDER BY name`)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, name string
		var quota, inCost, outCost int
		if e := rows.Scan(&id, &name, &quota, &inCost, &outCost); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "name": name, "monthlyRequests": quota, "inputCostMicrosPer1K": inCost, "outputCostMicrosPer1K": outCost})
	}
	c.JSON(200, gin.H{"items": out})
}

func (h *Handler) UpsertPlan(c *gin.Context) {
	var in struct {
		Name                                                         string `json:"name"`
		MonthlyRequests, InputCostMicrosPer1K, OutputCostMicrosPer1K int
	}
	if c.ShouldBindJSON(&in) != nil || in.Name == "" || in.MonthlyRequests < 0 || in.InputCostMicrosPer1K < 0 || in.OutputCostMicrosPer1K < 0 {
		c.JSON(400, gin.H{"error": "invalid plan"})
		return
	}
	_, e := h.db.Exec(c, `INSERT INTO ai_plans(id,name,monthly_requests,input_cost_micros_per_1k,output_cost_micros_per_1k) VALUES($1,$2,$3,$4,$5) ON CONFLICT(name) DO UPDATE SET monthly_requests=EXCLUDED.monthly_requests,input_cost_micros_per_1k=EXCLUDED.input_cost_micros_per_1k,output_cost_micros_per_1k=EXCLUDED.output_cost_micros_per_1k`, uuid.New(), in.Name, in.MonthlyRequests, in.InputCostMicrosPer1K, in.OutputCostMicrosPer1K)
	if e != nil {
		c.JSON(409, gin.H{"error": "plan already exists or invalid"})
		return
	}
	c.Status(201)
}

func (h *Handler) AssignPlan(c *gin.Context) {
	uid, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid user id"})
		return
	}
	var in struct {
		PlanID string `json:"planId"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "planId is required"})
		return
	}
	pid, e := uuid.Parse(in.PlanID)
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid plan id"})
		return
	}
	tag, e := h.db.Exec(c, `UPDATE users SET ai_plan_id=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND EXISTS(SELECT 1 FROM ai_plans WHERE id=$2)`, uid, pid)
	if e != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "user or plan not found"})
		return
	}
	c.Status(204)
	h.audit(c, "user.ai_plan.update", uid.String(), uuid.MustParse(c.GetString("userID")))
}

func (h *Handler) AuditLogs(c *gin.Context) {
	rows, e := h.db.Query(c, `SELECT id,user_id,action,resource,metadata,ip,created_at FROM audit_logs ORDER BY created_at DESC LIMIT 200`)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, action, res, ip string
		var uid any
		var meta []byte
		var created any
		if e := rows.Scan(&id, &uid, &action, &res, &meta, &ip, &created); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "userId": uid, "action": action, "resource": res, "metadata": meta, "ip": ip, "createdAt": created})
	}
	c.JSON(200, gin.H{"items": out})
}

func (h *Handler) SetRole(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if c.ShouldBindJSON(&in) != nil || (in.Role != "student" && in.Role != "parent" && in.Role != "admin") {
		c.JSON(400, gin.H{"error": "invalid role"})
		return
	}
	tag, e := h.db.Exec(c, `UPDATE users SET role=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, in.Role)
	if e != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	c.Status(204)
	h.audit(c, "user.role.update", id.String(), uuid.MustParse(c.GetString("userID")))
}
func (h *Handler) SetDisabled(c *gin.Context) {
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	var tag pgconn.CommandTag
	if in.Disabled {
		tag, e = h.db.Exec(c, `UPDATE users SET deleted_at=NOW(),updated_at=NOW() WHERE id=$1`, id)
	} else {
		tag, e = h.db.Exec(c, `UPDATE users SET deleted_at=NULL,updated_at=NOW() WHERE id=$1`, id)
	}
	if e != nil || tag.RowsAffected() == 0 {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}
	c.Status(204)
	h.audit(c, "user.disabled.update", id.String(), uuid.MustParse(c.GetString("userID")))
}
