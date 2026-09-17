package config

import "fmt"

func (c Config) Validate() error {
	if c.JWTSecret == "" || c.JWTSecret == "dev-only-change-me" {
		return fmt.Errorf("K12EDU_JWT_SECRET must be changed from the development default")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("K12EDU_DATABASE_URL is required")
	}
	if c.AIMonthlyRequests < 0 || c.AIInputCostMicrosPer1K < 0 || c.AIOutputCostMicrosPer1K < 0 {
		return fmt.Errorf("AI quota and prices must be non-negative")
	}
	return nil
}
