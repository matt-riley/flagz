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

type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	GRPCAddr           string
	StreamPollInterval time.Duration
}

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
