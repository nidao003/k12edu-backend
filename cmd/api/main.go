package main

import (
	"context"
	"log"
	"time"

	"github.com/nidao003/k12edu-backend/internal/auth"
	"github.com/nidao003/k12edu-backend/internal/cache"
	"github.com/nidao003/k12edu-backend/internal/config"
	"github.com/nidao003/k12edu-backend/internal/db"
	"github.com/nidao003/k12edu-backend/internal/httpapi"
	"github.com/nidao003/k12edu-backend/internal/mail"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Printf("configuration warning: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	rdb, redisErr := cache.Open(ctx, cfg.RedisURL)
	if redisErr != nil {
		log.Printf("redis disabled: %v", redisErr)
	}
	if rdb != nil {
		defer rdb.Close()
	}
	var authService *auth.Service
	if err != nil {
		log.Printf("database disabled: %v", err)
	} else {
		defer pool.Close()
		if err := db.Migrate(ctx, pool); err != nil {
			log.Fatal(err)
		}
		db.StartMaintenance(context.Background(), pool, cfg.RetentionDays)
		authService = auth.NewService(pool, cfg.JWTSecret, cfg.AppleClientID)
		mailer := mail.Sender{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.SMTPFrom}
		authService.SetMailer(mailer.Send)
		if err := authService.BootstrapAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("k12edu backend listening on %s", cfg.Addr)
	if err := httpapi.NewRouter(pool, authService, cfg.AIBaseURL, cfg.AIAPIKey, rdb).Run(cfg.Addr); err != nil {
		log.Fatal(err)
	}
}
