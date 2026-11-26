package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseDriver     string
	DatabaseURL        string
	RedisURL           string
	Prefix             string
	Workers            int
	QueueSize          int
	LogLevel           string
	DashboardPassword  string
	Port               string
	PortExplicit       bool
	CORSAllowedOrigins []string
}

func Load() (Config, error) {
	rawPort := strings.TrimSpace(os.Getenv("PORT"))
	port := rawPort
	if port == "" {
		port = "8080"
	} else {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return Config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
	}

	cfg := Config{
		DatabaseDriver:     strings.ToLower(env("DATABASE_DRIVER", "sqlite3")),
		DatabaseURL:        env("DATABASE_URL", "file:./data/sonicos.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"),
		RedisURL:           env("REDIS_URL", "redis://localhost:6379/0"),
		Prefix:             env("BOT_PREFIX", "!"),
		LogLevel:           strings.ToLower(env("LOG_LEVEL", "info")),
		DashboardPassword:  strings.TrimSpace(os.Getenv("DASHBOARD_PASSWORD")),
		Port:               port,
		PortExplicit:       rawPort != "",
		CORSAllowedOrigins: splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
	}

	var err error
	cfg.Workers, err = envInt("WORKERS", 8)
	if err != nil {
		return Config{}, err
	}
	cfg.QueueSize, err = envInt("QUEUE_SIZE", 512)
	if err != nil {
		return Config{}, err
	}
	if cfg.Workers < 1 || cfg.Workers > 256 {
		return Config{}, fmt.Errorf("WORKERS must be between 1 and 256")
	}
	if cfg.QueueSize < cfg.Workers {
		return Config{}, fmt.Errorf("QUEUE_SIZE must be at least WORKERS")
	}
	if cfg.Prefix == "" {
		return Config{}, fmt.Errorf("BOT_PREFIX cannot be empty")
	}
	if cfg.DatabaseDriver != "sqlite3" && cfg.DatabaseDriver != "pgx" {
		return Config{}, fmt.Errorf("DATABASE_DRIVER must be sqlite3 or pgx")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL cannot be empty")
	}
	if cfg.RedisURL == "" {
		return Config{}, fmt.Errorf("REDIS_URL cannot be empty")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, strings.TrimRight(trimmed, "/"))
		}
	}
	return out
}
