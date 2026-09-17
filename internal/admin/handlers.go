package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

type Handler struct{ db *pgxpool.Pool }

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
	rows, e := h.db.Query(c, `SELECT id,email,display_name,role,created_at FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT 100`)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var id, email, name, role string
		var created any
		if e := rows.Scan(&id, &email, &name, &role, &created); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"id": id, "email": email, "displayName": name, "role": role, "createdAt": created})
	}
	c.JSON(200, gin.H{"users": out})
}

func (h *Handler) AIUsage(c *gin.Context) {
	rows, e := h.db.Query(c, `SELECT model,COUNT(*),COALESCE(SUM(input_bytes),0),COALESCE(SUM(output_bytes),0) FROM ai_usage WHERE created_at>NOW()-INTERVAL '30 days' GROUP BY model ORDER BY COUNT(*) DESC`)
	if e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := make([]gin.H, 0)
	for rows.Next() {
		var model string
		var calls, inBytes, outBytes int64
		if e := rows.Scan(&model, &calls, &inBytes, &outBytes); e != nil {
			c.JSON(500, gin.H{"error": "scan failed"})
			return
		}
		out = append(out, gin.H{"model": model, "calls": calls, "inputBytes": inBytes, "outputBytes": outBytes})
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
}
