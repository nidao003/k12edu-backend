package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr                                                                                             string
	DatabaseURL                                                                                      string
	RedisURL                                                                                         string
	JWTSecret                                                                                        string
	AIBaseURL                                                                                        string
	AIAPIKey                                                                                         string
	AdminEmail                                                                                       string
	AdminPassword                                                                                    string
	AIMonthlyRequests                                                                                int
	AIInputCostMicrosPer1K                                                                           int
	AIOutputCostMicrosPer1K                                                                          int
	AIMonthlyCostLimitMicros                                                                         int
	AppleClientID                                                                                    string
	AllowedOrigins                                                                                   string
	LogLevel                                                                                         string
	SMTPHost, SMTPPort, SMTPUser, SMTPPassword, SMTPFrom                                             string
	StorageDriver, StorageEndpoint, StorageAccessKey, StorageSecretKey, StorageBucket, StorageRegion string
	StorageSSL                                                                                       bool
	RetentionDays                                                                                    int
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
	quota := envInt("K12EDU_AI_MONTHLY_REQUESTS", 500)
	inCost := envInt("K12EDU_AI_INPUT_COST_MICROS_PER_1K", 100)
	outCost := envInt("K12EDU_AI_OUTPUT_COST_MICROS_PER_1K", 300)
	monthCostLimit := envInt("K12EDU_AI_MONTHLY_COST_LIMIT_MICROS", 0)
	retention := envInt("K12EDU_RETENTION_DAYS", 365)
	ssl, _ := strconv.ParseBool(os.Getenv("K12EDU_STORAGE_SSL"))
	return Config{Addr: addr, DatabaseURL: databaseURL, RedisURL: os.Getenv("K12EDU_REDIS_URL"), JWTSecret: secret, AIBaseURL: os.Getenv("K12EDU_AI_BASE_URL"), AIAPIKey: os.Getenv("K12EDU_AI_API_KEY"), AdminEmail: os.Getenv("K12EDU_ADMIN_EMAIL"), AdminPassword: os.Getenv("K12EDU_ADMIN_PASSWORD"), AIMonthlyRequests: quota, AIInputCostMicrosPer1K: inCost, AIOutputCostMicrosPer1K: outCost, AIMonthlyCostLimitMicros: monthCostLimit, AppleClientID: os.Getenv("K12EDU_APPLE_CLIENT_ID"), AllowedOrigins: os.Getenv("K12EDU_ALLOWED_ORIGINS"), LogLevel: os.Getenv("K12EDU_LOG_LEVEL"), SMTPHost: os.Getenv("K12EDU_SMTP_HOST"), SMTPPort: os.Getenv("K12EDU_SMTP_PORT"), SMTPUser: os.Getenv("K12EDU_SMTP_USER"), SMTPPassword: os.Getenv("K12EDU_SMTP_PASSWORD"), SMTPFrom: os.Getenv("K12EDU_SMTP_FROM"), StorageDriver: os.Getenv("K12EDU_STORAGE_DRIVER"), StorageEndpoint: os.Getenv("K12EDU_STORAGE_ENDPOINT"), StorageAccessKey: os.Getenv("K12EDU_STORAGE_ACCESS_KEY"), StorageSecretKey: os.Getenv("K12EDU_STORAGE_SECRET_KEY"), StorageBucket: os.Getenv("K12EDU_STORAGE_BUCKET"), StorageRegion: os.Getenv("K12EDU_STORAGE_REGION"), StorageSSL: ssl, RetentionDays: retention}
}
func envInt(key string, fallback int) int {
	if n, e := strconv.Atoi(os.Getenv(key)); e == nil {
		return n
	}
	return fallback
}
