// Package config loads and validates the API configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// MinAdminTokenLen is the minimum length of ADMIN_TOKEN (SPEC.md §12).
const MinAdminTokenLen = 24

type Config struct {
	DatabaseURL    string
	APIAddr        string
	MigrateOnStart bool
	LogLevel       slog.Level

	// AdminToken protects /api/admin/*. Never log it.
	AdminToken string

	AIGRPCAddr         string
	AnalysisWorkers    int
	AnalysisTimeout    time.Duration
	AIDispatchAttempts int
}

// Load reads the configuration from the environment. All problems are
// reported together so a misconfigured deployment fails with one clear message.
func Load() (Config, error) {
	var errs []error

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIAddr:     getenv("API_ADDR", ":8080"),
		AdminToken:  strings.TrimSpace(os.Getenv("ADMIN_TOKEN")),
		AIGRPCAddr:  getenv("AI_GRPC_ADDR", "localhost:50051"),
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if len(cfg.AdminToken) < MinAdminTokenLen {
		errs = append(errs, fmt.Errorf("ADMIN_TOKEN is required and must be at least %d characters (generate one with: openssl rand -hex 24)", MinAdminTokenLen))
	}

	migrate, err := strconv.ParseBool(getenv("MIGRATE_ON_START", "true"))
	if err != nil {
		errs = append(errs, fmt.Errorf("MIGRATE_ON_START: %w", err))
	}
	cfg.MigrateOnStart = migrate

	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	cfg.AnalysisWorkers = intInRange(&errs, "ANALYSIS_WORKERS", 2, 1, 64)
	cfg.AIDispatchAttempts = intInRange(&errs, "AI_DISPATCH_ATTEMPTS", 3, 1, 10)

	timeout, err := time.ParseDuration(getenv("ANALYSIS_TIMEOUT", "300s"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("ANALYSIS_TIMEOUT: %w", err))
	case timeout < time.Second || timeout > time.Hour:
		errs = append(errs, errors.New("ANALYSIS_TIMEOUT must be between 1s and 1h"))
	}
	cfg.AnalysisTimeout = timeout

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func intInRange(errs *[]error, key string, fallback, lo, hi int) int {
	n, err := strconv.Atoi(getenv(key, strconv.Itoa(fallback)))
	if err != nil || n < lo || n > hi {
		*errs = append(*errs, fmt.Errorf("%s must be an integer between %d and %d", key, lo, hi))
	}
	return n
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
