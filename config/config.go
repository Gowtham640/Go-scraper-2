package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all environment variables
type Config struct {
	SupabaseURL      string
	SupabaseKey      string
	EncryptionKey    string
	Port             string
	AuthServicePort  string // Playwright auth-browser (see auth-browser/login.js)
	URL              string
	CronSecret       string
}

var AppConfig *Config

// LoadConfig loads environment variables from .env file
func LoadConfig() (*Config, error) {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using environment variables")
	}

	config := &Config{
		SupabaseURL:     getEnv("SUPABASE_URL", ""),
		SupabaseKey:     getEnv("SUPABASE_KEY", ""),
		EncryptionKey:   getEnv("ENCRYPTION_KEY", ""),
		Port:            getEnv("PORT", "8080"),
		AuthServicePort: getEnv("AUTH_SERVICE_PORT", "3001"),
		URL:             getEnv("URL", "http://localhost:3000"),
		CronSecret:      getEnv("CRON_SECRET", ""),
	}

	// Validate required fields
	if config.SupabaseURL == "" {
		return nil, fmt.Errorf("SUPABASE_URL is required")
	}
	if config.SupabaseKey == "" {
		return nil, fmt.Errorf("SUPABASE_KEY is required")
	}
	if config.EncryptionKey == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required")
	}

	AppConfig = config
	return config, nil
}

// getEnv retrieves environment variable with a fallback default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
