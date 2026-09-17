package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nidao003/k12edu-backend/internal/auth"
	syncapi "github.com/nidao003/k12edu-backend/internal/sync"
)

func NewRouter(pool *pgxpool.Pool, authService *auth.Service) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "k12edu-backend", "time": time.Now().UTC()})
	})

	v1 := r.Group("/api/v1")
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	if authService != nil {
		h := auth.NewHandler(authService)
		a := v1.Group("/auth")
		a.POST("/register", h.Register)
		a.POST("/login", h.Login)
		v1.GET("/me", h.RequireAuth(), h.Me)
		if pool != nil {
			sh := syncapi.NewHandler(pool)
			protected := v1.Group("/sync", h.RequireAuth())
			protected.GET("/progress", sh.GetProgress)
			protected.PUT("/progress", sh.PutProgress)
			protected.POST("/events", sh.AppendEvents)
		}
	}

	r.StaticFile("/admin", "admin/index.html")
	r.StaticFile("/admin/", "admin/index.html")

	return r
}
