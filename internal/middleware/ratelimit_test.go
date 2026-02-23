package middleware

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_AllowBeforeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 5)
	defer rl.Stop()

	if !rl.Allow("192.168.1.1") {
		t.Fatal("Allow should return true for unknown IP")
	}
}

func TestRateLimiter_AllowAfterSingleFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 5)
	defer rl.Stop()

	rl.RecordFailure("192.168.1.1")
	// Burst is 5, one consumed by RecordFailure, so Allow should still pass
	if !rl.Allow("192.168.1.1") {
		t.Fatal("Allow should return true after single failure with burst 5")
	}
}

func TestRateLimiter_ExceedLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 3)
	defer rl.Stop()

	for i := 0; i < 3; i++ {
		rl.RecordFailure("10.0.0.1")
	}

	// All burst tokens consumed; Allow should now fail
	if rl.Allow("10.0.0.1") {
		t.Fatal("Allow should return false after exceeding limit")
	}
}

func TestRateLimiter_DifferentIPsIndependent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 2)
	defer rl.Stop()

	// Exhaust IP1
	for i := 0; i < 2; i++ {
		rl.RecordFailure("10.0.0.1")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("10.0.0.1 should be rate limited")
	}

	// IP2 should still be allowed
	if !rl.Allow("10.0.0.2") {
		t.Fatal("10.0.0.2 should not be rate limited")
	}
}

func TestRateLimiter_DefaultMaxAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 0) // should default to 10
	defer rl.Stop()

	for i := 0; i < DefaultMaxAttemptsPerMinute; i++ {
		rl.RecordFailure("10.0.0.1")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("should be rate limited after default max attempts")
	}
}

func TestRateLimiter_MaxTrackedIPs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 5)
	defer rl.Stop()
	rl.maxTrackedIPs = 3

	rl.RecordFailure("1.1.1.1")
	rl.RecordFailure("2.2.2.2")
	rl.RecordFailure("3.3.3.3")
	// Adding a 4th should evict the oldest
	rl.RecordFailure("4.4.4.4")

	rl.mu.Lock()
	count := len(rl.entries)
	rl.mu.Unlock()
	if count > 3 {
		t.Fatalf("expected at most 3 tracked IPs, got %d", count)
	}
}

func TestRateLimiter_RemoveStale(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 5)
	defer rl.Stop()

	rl.RecordFailure("stale.ip")
	// Manually backdate the entry
	rl.mu.Lock()
	rl.entries["stale.ip"].lastSeen = time.Now().Add(-10 * time.Minute)
	rl.mu.Unlock()

	rl.removeStale()

	rl.mu.Lock()
	_, exists := rl.entries["stale.ip"]
	rl.mu.Unlock()
	if exists {
		t.Fatal("expected stale entry to be removed")
	}
}

func TestRateLimiter_StopCancelsCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl := NewRateLimiter(ctx, 5)
	rl.Stop()
	// Should not panic or block
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"", ""},
	}
	for _, tt := range tests {
		got := ExtractIP(tt.input)
		if got != tt.want {
			t.Errorf("ExtractIP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
