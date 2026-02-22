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
	defaultHTTPAddr                   = ":8080"
	defaultGRPCAddr                   = ":9090"
	defaultStreamPollInterval         = time.Second
	defaultTSStateDir                 = "tsnet-state"
	defaultMaxJSONBodySize      int64 = 1 << 20 // 1MB
	defaultEventBatchSize             = 1000
	defaultCacheResyncInterval        = time.Minute
)

// Config holds the runtime configuration for the flagz server.
type Config struct {
	DatabaseURL            string
	HTTPAddr               string
	GRPCAddr               string
	StreamPollInterval     time.Duration
	LogLevel               string
	AdminHostname          string
	TSAuthKey              string
	TSStateDir             string
	SessionSecret          string
	MaxJSONBodySize        int64
	EventBatchSize         int
	CacheResyncInterval    time.Duration
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

	// Admin Portal Config
	adminHostname := strings.TrimSpace(os.Getenv("ADMIN_HOSTNAME"))
	if adminHostname != "" && sessionSecret == "" {
		return Config{}, errors.New("SESSION_SECRET is required when ADMIN_HOSTNAME is set")
	}
	if adminHostname != "" && len(sessionSecret) < 32 {
		return Config{}, errors.New("SESSION_SECRET must be at least 32 characters when ADMIN_HOSTNAME is set")
	}

	maxJSONBodySize := defaultMaxJSONBodySize
	if v := strings.TrimSpace(os.Getenv("MAX_JSON_BODY_SIZE")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return Config{}, errors.New("MAX_JSON_BODY_SIZE must be a positive integer (bytes)")
		}
		maxJSONBodySize = n
	}

	eventBatchSize := defaultEventBatchSize
	if v := strings.TrimSpace(os.Getenv("EVENT_BATCH_SIZE")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, errors.New("EVENT_BATCH_SIZE must be a positive integer")
		}
		eventBatchSize = n
	}

	cacheResyncInterval := defaultCacheResyncInterval
	if v := strings.TrimSpace(os.Getenv("CACHE_RESYNC_INTERVAL")); v != "" {
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse CACHE_RESYNC_INTERVAL: %w", err)
		}
		if parsed <= 0 {
			return Config{}, errors.New("CACHE_RESYNC_INTERVAL must be > 0")
		}
		cacheResyncInterval = parsed
	}

	return Config{
		DatabaseURL:         databaseURL,
		HTTPAddr:            envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		GRPCAddr:            envOrDefault("GRPC_ADDR", defaultGRPCAddr),
		StreamPollInterval:  streamPollInterval,
		LogLevel:            envOrDefault("LOG_LEVEL", "info"),
		AdminHostname:       adminHostname,
		TSAuthKey:           os.Getenv("TS_AUTH_KEY"),
		TSStateDir:          envOrDefault("TS_STATE_DIR", defaultTSStateDir),
		SessionSecret:       sessionSecret,
		MaxJSONBodySize:     maxJSONBodySize,
		EventBatchSize:      eventBatchSize,
		CacheResyncInterval: cacheResyncInterval,
	}, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
