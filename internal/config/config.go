package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds application configuration.
type Config struct {
	// Server
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Workers
	DefaultWorkers int

	// Database
	DBPath string

	// Misc
	LogLevel string
}

// Load returns configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Addr:           getEnv("CONCURRO_ADDR", ":8080"),
		ReadTimeout:    getDurationEnv("CONCURRO_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:   getDurationEnv("CONCURRO_WRITE_TIMEOUT", 30*time.Second),
		DefaultWorkers: getIntEnv("CONCURRO_DEFAULT_WORKERS", 8),
		DBPath:         getEnv("CONCURRO_DB_PATH", "concurro.db"),
		LogLevel:       getEnv("CONCURRO_LOG_LEVEL", "info"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getIntEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
