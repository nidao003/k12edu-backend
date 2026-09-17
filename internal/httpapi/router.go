package httpapi

import (
	"context"
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

func NewRouter(pool *pgxpool.Pool, authService *auth.Service, aiBaseURL, aiAPIKey string, aiQuota, aiInputCost, aiOutputCost, aiMonthlyCostLimit int, rdb ...*redis.Client) *gin.Engine {
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
		status := http.StatusOK
		checks := gin.H{"database": "disabled", "redis": "disabled", "ai": "disabled"}
		if pool != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
			if err := pool.Ping(ctx); err != nil {
				checks["database"] = "unavailable"
				status = http.StatusServiceUnavailable
			} else {
				checks["database"] = "ok"
			}
			cancel()
		}
		if len(rdb) > 0 && rdb[0] != nil {
			if err := rdb[0].Ping(c).Err(); err != nil {
				checks["redis"] = "unavailable"
			} else {
				checks["redis"] = "ok"
			}
		}
		if aiBaseURL != "" && aiAPIKey != "" {
			checks["ai"] = "configured"
		}
		c.JSON(status, gin.H{"status": map[bool]string{true: "ok", false: "degraded"}[status == http.StatusOK], "service": "k12edu-backend", "checks": checks, "time": time.Now().UTC()})
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
		a.POST("/verify-email", h.VerifyEmail)
		a.POST("/forgot-password", h.ForgotPassword)
		a.POST("/reset-password", h.ResetPassword)
		a.POST("/refresh", h.Refresh)
		a.POST("/revoke", h.Revoke)
		v1.GET("/me", h.RequireAuth(), h.Me)
		v1.GET("/me/sessions", h.RequireAuth(), h.Sessions)
		v1.DELETE("/me/sessions/:id", h.RequireAuth(), h.RevokeSession)
		v1.POST("/me/password", h.RequireAuth(), h.ChangePassword)
		v1.POST("/me/apple/link", h.RequireAuth(), h.LinkApple)
		v1.DELETE("/me/apple", h.RequireAuth(), h.UnlinkApple)
		v1.GET("/me/export", h.RequireAuth(), h.Export)
		v1.DELETE("/me", h.RequireAuth(), h.DeleteAccount)
		v1.DELETE("/me/permanent", h.RequireAuth(), h.HardDelete)
		if store, err := storage.NewConfigured(); err == nil && pool != nil {
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
			protected.POST("/events/compact", sh.CompactEvents)
			protected.GET("/events/replay", sh.ReplayEvents)
		}
		if pool != nil {
			ch := content.NewHandler(pool, rdb...)
			v1.GET("/content", ch.Published)
			contentRoutes := v1.Group("/admin/content", h.RequireAuth(), admin.RequirePermission(pool, "admin.write"))
			contentRoutes.GET("", ch.List)
			contentRoutes.POST("", ch.Create)
			contentRoutes.PUT("/:id", ch.Update)
			contentRoutes.DELETE("/:id", ch.Delete)
		}
		if pool != nil {
			ah := admin.NewHandler(pool)
			adminRoutes := v1.Group("/admin", h.RequireAuth(), admin.RequirePermission(pool, "admin.read"))
			adminWrite := adminRoutes.Group("", admin.RequirePermission(pool, "admin.write"))
			adminRoutes.GET("/stats", ah.Stats)
			adminRoutes.GET("/users", ah.Users)
			adminRoutes.GET("/ai-usage", ah.AIUsage)
			adminRoutes.GET("/ai-billing", ah.AIBilling)
			adminRoutes.GET("/ai-safety-events", ah.AISafetyEvents)
			adminRoutes.GET("/file-safety", ah.FileSafety)
			adminRoutes.GET("/audit-logs", ah.AuditLogs)
			adminRoutes.GET("/ai-plans", ah.Plans)
			adminWrite.POST("/ai-plans", ah.UpsertPlan)
			adminWrite.POST("/users/:id/ai-credit", ah.CreditAI)
			adminWrite.PATCH("/users/:id/ai-plan", ah.AssignPlan)
			adminWrite.PATCH("/ai-safety-events/:id", ah.ReviewSafety)
			adminWrite.PATCH("/users/:id/role", ah.SetRole)
			adminWrite.PATCH("/users/:id/disabled", ah.SetDisabled)
			adminWrite.PATCH("/users/:id/minor-mode", ah.SetMinorMode)
			adminWrite.PATCH("/file-safety/:id", ah.ReviewFile)
			adminWrite.PATCH("/users/:id/permission", ah.GrantPermission)
			adminWrite.DELETE("/users/:id/permission", ah.RevokePermission)
		}
		aiHandler := ai.NewHandler(aiBaseURL, aiAPIKey, pool, aiQuota, aiInputCost, aiOutputCost, aiMonthlyCostLimit, rdb...)
		v1.GET("/ai/health", h.RequireAuth(), aiHandler.Health)
		v1.POST("/ai/chat/completions", h.RequireAuth(), aiHandler.Chat)
		v1.POST("/ai/safety/:id/appeal", h.RequireAuth(), aiHandler.AppealSafety)
	}

	r.GET("/admin", func(c *gin.Context) { adminPage(c) })
	r.GET("/admin/", func(c *gin.Context) { adminPage(c) })
	r.Static("/admin/assets", "admin/dist/assets")

	return r
}
