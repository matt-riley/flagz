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
	if cfg.MaxJSONBodySize != defaultMaxJSONBodySize {
		t.Errorf("MaxJSONBodySize = %d, want %d", cfg.MaxJSONBodySize, defaultMaxJSONBodySize)
	}
	if cfg.EventBatchSize != defaultEventBatchSize {
		t.Errorf("EventBatchSize = %d, want %d", cfg.EventBatchSize, defaultEventBatchSize)
	}
	if cfg.CacheResyncInterval != defaultCacheResyncInterval {
		t.Errorf("CacheResyncInterval = %v, want %v", cfg.CacheResyncInterval, defaultCacheResyncInterval)
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

func TestLoad_CustomConfigurableLimits(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("MAX_JSON_BODY_SIZE", "2048")
	t.Setenv("EVENT_BATCH_SIZE", "250")
	t.Setenv("CACHE_RESYNC_INTERVAL", "30s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxJSONBodySize != 2048 {
		t.Errorf("MaxJSONBodySize = %d, want 2048", cfg.MaxJSONBodySize)
	}
	if cfg.EventBatchSize != 250 {
		t.Errorf("EventBatchSize = %d, want 250", cfg.EventBatchSize)
	}
	if cfg.CacheResyncInterval != 30*time.Second {
		t.Errorf("CacheResyncInterval = %v, want 30s", cfg.CacheResyncInterval)
	}
}

func TestLoad_MaxJSONBodySize_Invalid(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")

	for _, tc := range []string{"not-a-number", "0", "-1"} {
		t.Run(tc, func(t *testing.T) {
			t.Setenv("MAX_JSON_BODY_SIZE", tc)
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should fail for MAX_JSON_BODY_SIZE=%q", tc)
			}
		})
	}
}

func TestLoad_EventBatchSize_Invalid(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")

	for _, tc := range []string{"not-a-number", "0", "-1"} {
		t.Run(tc, func(t *testing.T) {
			t.Setenv("EVENT_BATCH_SIZE", tc)
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should fail for EVENT_BATCH_SIZE=%q", tc)
			}
		})
	}
}

func TestLoad_CacheResyncInterval_Invalid(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("CACHE_RESYNC_INTERVAL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail for invalid CACHE_RESYNC_INTERVAL")
	}
}

func TestLoad_CacheResyncInterval_Zero(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("CACHE_RESYNC_INTERVAL", "0s")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail for zero CACHE_RESYNC_INTERVAL")
	}
}

func TestLoad_CacheResyncInterval_Negative(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_HOSTNAME", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("CACHE_RESYNC_INTERVAL", "-1s")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail for negative CACHE_RESYNC_INTERVAL")
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
