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
	"strings"
	"time"
)

const (
	defaultHTTPAddr           = ":8080"
	defaultGRPCAddr           = ":9090"
	defaultStreamPollInterval = time.Second
)

// Config holds the runtime configuration for the flagz server.
type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	GRPCAddr           string
	StreamPollInterval time.Duration
}

// Load reads configuration from environment variables, applying defaults where
// appropriate. It returns an error if required variables are missing or if
// optional values fail validation.
func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

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

	return Config{
		DatabaseURL:        databaseURL,
		HTTPAddr:           envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		GRPCAddr:           envOrDefault("GRPC_ADDR", defaultGRPCAddr),
		StreamPollInterval: streamPollInterval,
	}, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
