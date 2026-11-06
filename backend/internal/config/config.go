// Package config loads and validates the API configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL    string
	APIAddr        string
	MigrateOnStart bool
	LogLevel       slog.Level
}

// Load reads the configuration from the environment. All problems are
// reported together so a misconfigured deployment fails with one clear message.
func Load() (Config, error) {
	var errs []error

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIAddr:     getenv("API_ADDR", ":8080"),
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}

	migrate, err := strconv.ParseBool(getenv("MIGRATE_ON_START", "true"))
	if err != nil {
		errs = append(errs, fmt.Errorf("MIGRATE_ON_START: %w", err))
	}
	cfg.MigrateOnStart = migrate

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
