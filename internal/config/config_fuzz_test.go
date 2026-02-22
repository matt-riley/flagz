package config

import (
	"strings"
	"testing"
	"time"
)

func FuzzEnvOrDefault(f *testing.F) {
	f.Add("", ":8080")
	f.Add("  :9090  ", ":8080")

	f.Fuzz(func(t *testing.T, value, fallback string) {
		if strings.ContainsRune(value, '\x00') {
			t.Skip()
		}

		const key = "FLAGZ_TEST_ENV_OR_DEFAULT"
		t.Setenv(key, value)

		got := envOrDefault(key, fallback)
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			if got != fallback {
				t.Fatalf("envOrDefault() = %q, want fallback %q", got, fallback)
			}
			return
		}

		if got != trimmed {
			t.Fatalf("envOrDefault() = %q, want trimmed value %q", got, trimmed)
		}
	})
}

func FuzzLoadStreamPollInterval(f *testing.F) {
	f.Add("")
	f.Add("1s")
	f.Add("0s")
	f.Add("-1s")
	f.Add("not-a-duration")

	f.Fuzz(func(t *testing.T, streamPollInterval string) {
		if strings.ContainsRune(streamPollInterval, '\x00') {
			t.Skip()
		}

		t.Setenv("DATABASE_URL", "postgres://localhost/test")
		t.Setenv("SESSION_SECRET", "test-secret")
		t.Setenv("HTTP_ADDR", "")
		t.Setenv("GRPC_ADDR", "")
		t.Setenv("STREAM_POLL_INTERVAL", streamPollInterval)

		cfg, err := Load()
		trimmed := strings.TrimSpace(streamPollInterval)
		if trimmed == "" {
			if err != nil {
				t.Fatalf("Load() error = %v, want nil for empty STREAM_POLL_INTERVAL", err)
			}
			if cfg.StreamPollInterval != defaultStreamPollInterval {
				t.Fatalf("StreamPollInterval = %s, want %s", cfg.StreamPollInterval, defaultStreamPollInterval)
			}
			return
		}

		parsed, parseErr := time.ParseDuration(trimmed)
		if parseErr != nil || parsed <= 0 {
			if err == nil {
				t.Fatalf("Load() error = nil, want non-nil for STREAM_POLL_INTERVAL=%q", streamPollInterval)
			}
			return
		}

		if err != nil {
			t.Fatalf("Load() error = %v, want nil for STREAM_POLL_INTERVAL=%q", err, streamPollInterval)
		}
		if cfg.StreamPollInterval != parsed {
			t.Fatalf("StreamPollInterval = %s, want %s", cfg.StreamPollInterval, parsed)
		}
	})
}
