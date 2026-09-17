package config

import "os"

type Config struct {
	Addr          string
	DatabaseURL   string
	RedisURL      string
	JWTSecret     string
	AIBaseURL     string
	AIAPIKey      string
	AdminEmail    string
	AdminPassword string
}

func Load() Config {
	addr := os.Getenv("K12EDU_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	databaseURL := os.Getenv("K12EDU_DATABASE_URL")
	secret := os.Getenv("K12EDU_JWT_SECRET")
	if secret == "" {
		secret = "dev-only-change-me"
	}
	return Config{Addr: addr, DatabaseURL: databaseURL, RedisURL: os.Getenv("K12EDU_REDIS_URL"), JWTSecret: secret, AIBaseURL: os.Getenv("K12EDU_AI_BASE_URL"), AIAPIKey: os.Getenv("K12EDU_AI_API_KEY"), AdminEmail: os.Getenv("K12EDU_ADMIN_EMAIL"), AdminPassword: os.Getenv("K12EDU_ADMIN_PASSWORD")}
}
