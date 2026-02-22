// Package config loads server configuration from environment variables.
//
// Required variables:
//   - DATABASE_URL: PostgreSQL connection string.
//
// Optional variables:
//   - HTTP_ADDR: listen address for the HTTP server (default ":8080").
//   - GRPC_ADDR: listen address for the gRPC server (default ":9090").
//   - STREAM_POLL_INTERVAL: polling interval for SSE and gRPC streaming
//     (default "1s", must be > 0 if set).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr           = ":8080"
	defaultGRPCAddr           = ":9090"
	defaultStreamPollInterval = time.Second
	defaultTSStateDir         = "tsnet-state"
	defaultAuthRateLimit      = 10
)

// Config holds the runtime configuration for the flagz server.
type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	GRPCAddr           string
	StreamPollInterval time.Duration
	LogLevel           string
	AuthRateLimit      int
	AdminHostname      string
	TSAuthKey          string
	TSStateDir         string
	SessionSecret      string
}

// Load reads configuration from environment variables, applying defaults where
// appropriate. It returns an error if required variables are missing or if
// optional values fail validation.
func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	sessionSecret := strings.TrimSpace(os.Getenv("SESSION_SECRET"))

	streamPollInterval := defaultStreamPollInterval
	if value := strings.TrimSpace(os.Getenv("STREAM_POLL_INTERVAL")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse STREAM_POLL_INTERVAL: %w", err)
		}
		if parsed <= 0 {
			return Config{}, errors.New("STREAM_POLL_INTERVAL must be > 0")
		}
		streamPollInterval = parsed
	}

	authRateLimit := defaultAuthRateLimit
	if value := strings.TrimSpace(os.Getenv("AUTH_RATE_LIMIT")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse AUTH_RATE_LIMIT: %w", err)
		}
		if parsed <= 0 {
			return Config{}, errors.New("AUTH_RATE_LIMIT must be > 0")
		}
		authRateLimit = parsed
	}

	// Admin Portal Config
	adminHostname := strings.TrimSpace(os.Getenv("ADMIN_HOSTNAME"))
	if adminHostname != "" && sessionSecret == "" {
		return Config{}, errors.New("SESSION_SECRET is required when ADMIN_HOSTNAME is set")
	}
	if adminHostname != "" && len(sessionSecret) < 32 {
		return Config{}, errors.New("SESSION_SECRET must be at least 32 characters when ADMIN_HOSTNAME is set")
	}

	return Config{
		DatabaseURL:        databaseURL,
		HTTPAddr:           envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		GRPCAddr:           envOrDefault("GRPC_ADDR", defaultGRPCAddr),
		StreamPollInterval: streamPollInterval,
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
		AuthRateLimit:      authRateLimit,
		AdminHostname:      adminHostname, // Default to empty (disabled)
		TSAuthKey:          os.Getenv("TS_AUTH_KEY"),
		TSStateDir:         envOrDefault("TS_STATE_DIR", defaultTSStateDir),
		SessionSecret:      sessionSecret,
	}, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
