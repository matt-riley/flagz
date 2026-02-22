package config

import (
	"testing"
	"time"
)

func TestLoad_RequiredDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail when DATABASE_URL is empty")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("STREAM_POLL_INTERVAL", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("GRPC_ADDR", "")
	t.Setenv("TS_AUTH_KEY", "")
	t.Setenv("TS_STATE_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.GRPCAddr != ":9090" {
		t.Errorf("GRPCAddr = %q, want :9090", cfg.GRPCAddr)
	}
	if cfg.StreamPollInterval != time.Second {
		t.Errorf("StreamPollInterval = %v, want 1s", cfg.StreamPollInterval)
	}
	if cfg.TSStateDir != "tsnet-state" {
		t.Errorf("TSStateDir = %q, want tsnet-state", cfg.TSStateDir)
	}
	if cfg.AuthRateLimit != 10 {
		t.Errorf("AuthRateLimit = %d, want 10", cfg.AuthRateLimit)
	}
}

func TestLoad_StreamPollInterval_Invalid(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("STREAM_POLL_INTERVAL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail for invalid STREAM_POLL_INTERVAL")
	}
}

func TestLoad_StreamPollInterval_Zero(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("STREAM_POLL_INTERVAL", "0s")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail for zero STREAM_POLL_INTERVAL")
	}
}

func TestLoad_StreamPollInterval_Negative(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("STREAM_POLL_INTERVAL", "-1s")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail for negative STREAM_POLL_INTERVAL")
	}
}

func TestLoad_AdminHostname_RequiresSessionSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "admin.example.com")
	t.Setenv("SESSION_SECRET", "")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail when ADMIN_HOSTNAME set without SESSION_SECRET")
	}
}

func TestLoad_AdminHostname_ShortSessionSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "admin.example.com")
	t.Setenv("SESSION_SECRET", "short")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail when SESSION_SECRET < 32 chars")
	}
}

func TestLoad_CustomAddrs(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("HTTP_ADDR", ":3000")
	t.Setenv("GRPC_ADDR", ":4000")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("STREAM_POLL_INTERVAL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":3000" {
		t.Errorf("HTTPAddr = %q, want :3000", cfg.HTTPAddr)
	}
	if cfg.GRPCAddr != ":4000" {
		t.Errorf("GRPCAddr = %q, want :4000", cfg.GRPCAddr)
	}
}

func TestLoad_CustomStreamPollInterval(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("STREAM_POLL_INTERVAL", "5s")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StreamPollInterval != 5*time.Second {
		t.Errorf("StreamPollInterval = %v, want 5s", cfg.StreamPollInterval)
	}
}

func TestEnvOrDefault_EmptyReturnsDefault(t *testing.T) {
	t.Setenv("TEST_KEY", "")
	got := envOrDefault("TEST_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("envOrDefault() = %q, want %q", got, "fallback")
	}
}

func TestEnvOrDefault_WhitespaceReturnsDefault(t *testing.T) {
	t.Setenv("TEST_KEY", "   ")
	got := envOrDefault("TEST_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("envOrDefault() = %q, want %q", got, "fallback")
	}
}

func TestEnvOrDefault_ValueReturnsValue(t *testing.T) {
	t.Setenv("TEST_KEY", " value ")
	got := envOrDefault("TEST_KEY", "fallback")
	if got != "value" {
		t.Errorf("envOrDefault() = %q, want %q", got, "value")
	}
}
