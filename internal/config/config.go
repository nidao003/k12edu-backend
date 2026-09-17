package config

import "os"

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
	AppleClientID                                                                                    string
	AllowedOrigins                                                                                   string
	LogLevel                                                                                         string
	SMTPHost, SMTPPort, SMTPUser, SMTPPassword, SMTPFrom                                             string
	StorageDriver, StorageEndpoint, StorageAccessKey, StorageSecretKey, StorageBucket, StorageRegion string
	StorageSSL                                                                                       bool
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
	return Config{Addr: addr, DatabaseURL: databaseURL, RedisURL: os.Getenv("K12EDU_REDIS_URL"), JWTSecret: secret, AIBaseURL: os.Getenv("K12EDU_AI_BASE_URL"), AIAPIKey: os.Getenv("K12EDU_AI_API_KEY"), AdminEmail: os.Getenv("K12EDU_ADMIN_EMAIL"), AdminPassword: os.Getenv("K12EDU_ADMIN_PASSWORD"), AIMonthlyRequests: 500, AIInputCostMicrosPer1K: 100, AIOutputCostMicrosPer1K: 300, AppleClientID: os.Getenv("K12EDU_APPLE_CLIENT_ID"), AllowedOrigins: os.Getenv("K12EDU_ALLOWED_ORIGINS"), LogLevel: os.Getenv("K12EDU_LOG_LEVEL"), SMTPHost: os.Getenv("K12EDU_SMTP_HOST"), SMTPPort: os.Getenv("K12EDU_SMTP_PORT"), SMTPUser: os.Getenv("K12EDU_SMTP_USER"), SMTPPassword: os.Getenv("K12EDU_SMTP_PASSWORD"), SMTPFrom: os.Getenv("K12EDU_SMTP_FROM"), StorageDriver: os.Getenv("K12EDU_STORAGE_DRIVER"), StorageEndpoint: os.Getenv("K12EDU_STORAGE_ENDPOINT"), StorageAccessKey: os.Getenv("K12EDU_STORAGE_ACCESS_KEY"), StorageSecretKey: os.Getenv("K12EDU_STORAGE_SECRET_KEY"), StorageBucket: os.Getenv("K12EDU_STORAGE_BUCKET"), StorageRegion: os.Getenv("K12EDU_STORAGE_REGION")}
}
