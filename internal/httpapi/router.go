package httpapi

import (
	"github.com/google/uuid"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nidao003/k12edu-backend/internal/admin"
	"github.com/nidao003/k12edu-backend/internal/ai"
	"github.com/nidao003/k12edu-backend/internal/auth"
	"github.com/nidao003/k12edu-backend/internal/content"
	"github.com/nidao003/k12edu-backend/internal/storage"
	syncapi "github.com/nidao003/k12edu-backend/internal/sync"
	"github.com/redis/go-redis/v9"
)

func NewRouter(pool *pgxpool.Pool, authService *auth.Service, aiBaseURL, aiAPIKey string, rdb ...*redis.Client) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(MetricsMiddleware())
	r.Use(func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Origin", c.GetHeader("Origin"))
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "k12edu-backend", "time": time.Now().UTC()})
	})
	r.GET("/metrics", Metrics)
	r.StaticFile("/openapi.yaml", "openapi.yaml")

	v1 := r.Group("/api/v1")
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	if authService != nil {
		h := auth.NewHandler(authService)
		a := v1.Group("/auth")
		a.POST("/register", h.Register)
		a.POST("/login", h.Login)
		a.POST("/apple", h.Apple)
		a.POST("/refresh", h.Refresh)
		a.POST("/revoke", h.Revoke)
		v1.GET("/me", h.RequireAuth(), h.Me)
		v1.POST("/me/password", h.RequireAuth(), h.ChangePassword)
		v1.GET("/me/export", h.RequireAuth(), h.Export)
		v1.DELETE("/me", h.RequireAuth(), h.DeleteAccount)
		v1.DELETE("/me/permanent", h.RequireAuth(), h.HardDelete)
		if store, err := storage.NewLocal("./data/files"); err == nil && pool != nil {
			files := storageHandler{store: store, db: pool}
			v1.POST("/me/files", h.RequireAuth(), files.Upload)
			v1.GET("/me/files", h.RequireAuth(), files.List)
			v1.GET("/me/files/:id", h.RequireAuth(), files.Download)
			v1.DELETE("/me/files/:id", h.RequireAuth(), files.Delete)
		}
		if pool != nil {
			sh := syncapi.NewHandler(pool)
			protected := v1.Group("/sync", h.RequireAuth())
			protected.GET("/progress", sh.GetProgress)
			protected.PUT("/progress", sh.PutProgress)
			protected.POST("/merge", sh.Merge)
			protected.POST("/events", sh.AppendEvents)
			protected.POST("/devices", sh.RegisterDevice)
			protected.GET("/events", sh.Events)
			protected.POST("/cursor", sh.Acknowledge)
			protected.POST("/events/archive", sh.ArchiveEvents)
			protected.GET("/events/replay", sh.ReplayEvents)
		}
		if pool != nil {
			ch := content.NewHandler(pool, rdb...)
			v1.GET("/content", ch.Published)
			contentRoutes := v1.Group("/admin/content", h.RequireAuth(), admin.RequireAdmin())
			contentRoutes.GET("", ch.List)
			contentRoutes.POST("", ch.Create)
			contentRoutes.PUT("/:id", ch.Update)
			contentRoutes.DELETE("/:id", ch.Delete)
		}
		if pool != nil {
			ah := admin.NewHandler(pool)
			adminRoutes := v1.Group("/admin", h.RequireAuth(), admin.RequireAdmin())
			adminRoutes.GET("/stats", ah.Stats)
			adminRoutes.GET("/users", ah.Users)
			adminRoutes.GET("/ai-usage", ah.AIUsage)
			adminRoutes.GET("/ai-safety-events", ah.AISafetyEvents)
			adminRoutes.GET("/audit-logs", ah.AuditLogs)
			adminRoutes.GET("/ai-plans", ah.Plans)
			adminRoutes.POST("/ai-plans", ah.UpsertPlan)
			adminRoutes.PATCH("/users/:id/ai-plan", ah.AssignPlan)
			adminRoutes.PATCH("/ai-safety-events/:id", ah.ReviewSafety)
			adminRoutes.PATCH("/users/:id/role", ah.SetRole)
			adminRoutes.PATCH("/users/:id/disabled", ah.SetDisabled)
		}
		aiHandler := ai.NewHandler(aiBaseURL, aiAPIKey, pool, 500, 100, 300, rdb...)
		v1.GET("/ai/health", h.RequireAuth(), aiHandler.Health)
		v1.POST("/ai/chat/completions", h.RequireAuth(), aiHandler.Chat)
	}

	r.GET("/admin", func(c *gin.Context) { adminPage(c) })
	r.GET("/admin/", func(c *gin.Context) { adminPage(c) })
	r.Static("/admin/assets", "admin/dist/assets")

	return r
}
