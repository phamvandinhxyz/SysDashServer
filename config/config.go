package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port            string
	JWTSecret       string
	TokenDuration   time.Duration
	DBPath          string
	MetricInterval  time.Duration
	PersistInterval time.Duration
	RetentionDays   int
}

func LoadConfig() *Config {
	port := getEnv("PORT", "8095")
	jwtSecret := getEnv("JWT_SECRET", "sysdash-super-secret-key-production-change-me")
	dbPath := getEnv("DB_PATH", "sysdash.db")

	// Default retention: 365 days (1 year)
	retentionDays, err := strconv.Atoi(getEnv("RETENTION_DAYS", "365"))
	if err != nil {
		retentionDays = 365
	}

	return &Config{
		Port:            port,
		JWTSecret:       jwtSecret,
		TokenDuration:   24 * time.Hour,
		DBPath:          dbPath,
		MetricInterval:  2 * time.Second,  // Realtime streaming interval
		PersistInterval: 15 * time.Second, // Interval to save metrics to database
		RetentionDays:   retentionDays,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
