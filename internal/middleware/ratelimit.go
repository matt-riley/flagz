package middleware

import (
	"context"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultMaxAttemptsPerMinute is the default rate limit for failed auth attempts per IP.
	DefaultMaxAttemptsPerMinute = 10

	// DefaultMaxTrackedIPs is the maximum number of IPs tracked to prevent unbounded memory.
	DefaultMaxTrackedIPs = 10000

	cleanupInterval = time.Minute
	staleThreshold  = 5 * time.Minute
)

type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter tracks per-IP failed authentication attempts.
type RateLimiter struct {
	mu            sync.Mutex
	entries       map[string]*ipEntry
	maxPerMinute  int
	maxTrackedIPs int
	cancel        context.CancelFunc
}

// NewRateLimiter creates a new per-IP rate limiter with the given max attempts per minute.
// Pass 0 to use DefaultMaxAttemptsPerMinute.
func NewRateLimiter(ctx context.Context, maxPerMinute int) *RateLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = DefaultMaxAttemptsPerMinute
	}
	ctx, cancel := context.WithCancel(ctx)
	rl := &RateLimiter{
		entries:       make(map[string]*ipEntry),
		maxPerMinute:  maxPerMinute,
		maxTrackedIPs: DefaultMaxTrackedIPs,
		cancel:        cancel,
	}
	go rl.cleanup(ctx)
	return rl
}

// Allow reports whether the given IP is allowed to make another auth attempt.
// Returns false if the rate limit has been exceeded.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, ok := rl.entries[ip]
	if !ok {
		return true // no prior failures recorded
	}
	e.lastSeen = time.Now()
	return e.limiter.Allow()
}

// RecordFailure records a failed auth attempt for the given IP.
func (rl *RateLimiter) RecordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e := rl.getOrCreateEntryLocked(ip, time.Now())
	e.limiter.Allow() // consume a token
}

// RecordFailureAndAllow records a failed attempt for ip and returns whether the
// attempt is still within the configured rate limit.
func (rl *RateLimiter) RecordFailureAndAllow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e := rl.getOrCreateEntryLocked(ip, time.Now())
	return e.limiter.Allow()
}

func (rl *RateLimiter) getOrCreateEntryLocked(ip string, now time.Time) *ipEntry {
	e, ok := rl.entries[ip]
	if !ok {
		if len(rl.entries) >= rl.maxTrackedIPs {
			rl.evictOldestLocked()
		}
		r := rate.Limit(float64(rl.maxPerMinute) / 60.0)
		e = &ipEntry{
			limiter:  rate.NewLimiter(r, rl.maxPerMinute),
			lastSeen: now,
		}
		rl.entries[ip] = e
	}
	e.lastSeen = now
	return e
}

// Stop cancels the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.cancel()
}

func (rl *RateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.removeStale()
		}
	}
}

func (rl *RateLimiter) removeStale() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for ip, e := range rl.entries {
		if now.Sub(e.lastSeen) > staleThreshold {
			delete(rl.entries, ip)
		}
	}
}

func (rl *RateLimiter) evictOldestLocked() {
	var oldestIP string
	var oldestTime time.Time
	first := true
	for ip, e := range rl.entries {
		if first || e.lastSeen.Before(oldestTime) {
			oldestIP = ip
			oldestTime = e.lastSeen
			first = false
		}
	}
	if oldestIP != "" {
		delete(rl.entries, oldestIP)
	}
}

// ExtractIP extracts the IP address from a RemoteAddr string, stripping the port.
func ExtractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr // already just an IP
	}
	return host
}
