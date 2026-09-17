package admin

import (
	"github.com/gin-gonic/gin"
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
	if e := h.db.QueryRow(c, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&users); e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	if e := h.db.QueryRow(c, `SELECT COUNT(DISTINCT user_id) FROM sync_events WHERE created_at > NOW()-INTERVAL '24 hours'`).Scan(&active); e != nil {
		c.JSON(500, gin.H{"error": "query failed"})
		return
	}
	c.JSON(200, gin.H{"users": users, "activeUsers24h": active})
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
