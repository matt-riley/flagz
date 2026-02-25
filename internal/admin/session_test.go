package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckLoginRateLimit(t *testing.T) {
	mgr := &SessionManager{
		loginAttempts: make(map[string][]time.Time),
	}

	ip := "192.168.1.1"

	// First attempt should be allowed
	if !mgr.CheckLoginRateLimit(ip) {
		t.Fatal("first attempt should be allowed")
	}

	// Record maxLoginAttempts failed attempts
	for i := 0; i < maxLoginAttempts; i++ {
		mgr.RecordLoginAttempt(ip)
	}

	// Next attempt should be blocked
	if mgr.CheckLoginRateLimit(ip) {
		t.Fatal("should be rate limited after max attempts")
	}

	// Different IP should still be allowed
	if !mgr.CheckLoginRateLimit("10.0.0.1") {
		t.Fatal("different IP should not be rate limited")
	}
}

func TestCheckLoginRateLimit_ExpiredAttempts(t *testing.T) {
	mgr := &SessionManager{
		loginAttempts: make(map[string][]time.Time),
	}

	ip := "192.168.1.1"

	// Record old attempts (beyond the login window)
	oldTime := time.Now().Add(-loginWindow - time.Minute)
	mgr.mu.Lock()
	for i := 0; i < maxLoginAttempts; i++ {
		mgr.loginAttempts[ip] = append(mgr.loginAttempts[ip], oldTime)
	}
	mgr.mu.Unlock()

	// Should be allowed because all attempts are expired
	if !mgr.CheckLoginRateLimit(ip) {
		t.Fatal("expired attempts should not count toward rate limit")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	mgr := &SessionManager{
		sessionSecret: []byte("test-secret-that-is-32-chars-lng"),
	}

	hash1 := mgr.hashToken("test-token")
	hash2 := mgr.hashToken("test-token")

	if hash1 != hash2 {
		t.Fatal("hashToken should be deterministic")
	}

	hash3 := mgr.hashToken("different-token")
	if hash1 == hash3 {
		t.Fatal("different tokens should produce different hashes")
	}
}

func TestHashTokenUsesSecret(t *testing.T) {
	mgr1 := &SessionManager{sessionSecret: []byte("secret-one-is-32-chars-long-xxx")}
	mgr2 := &SessionManager{sessionSecret: []byte("secret-two-is-32-chars-long-xxx")}

	hash1 := mgr1.hashToken("same-token")
	hash2 := mgr2.hashToken("same-token")

	if hash1 == hash2 {
		t.Fatal("same token with different secrets should produce different hashes")
	}
}

func TestSetSessionCookie(t *testing.T) {
	mgr := &SessionManager{}

	w := httptest.NewRecorder()
	mgr.SetSessionCookie(w, "test-token")

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if cookie.Value != "test-token" {
		t.Fatalf("cookie value = %q, want %q", cookie.Value, "test-token")
	}
	if !cookie.HttpOnly {
		t.Fatal("cookie should be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v, want Lax", cookie.SameSite)
	}
}

func TestClearSessionCookie(t *testing.T) {
	mgr := &SessionManager{}

	w := httptest.NewRecorder()
	mgr.ClearSessionCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Fatalf("cleared cookie name = %q, want %q", cookie.Name, sessionCookieName)
	}
	if cookie.Value != "" {
		t.Fatalf("cleared cookie should have empty value, got %q", cookie.Value)
	}
	if cookie.Path != "/" {
		t.Fatalf("cleared cookie path = %q, want /", cookie.Path)
	}
	if cookie.MaxAge != -1 {
		t.Fatalf("cleared cookie MaxAge = %d, want -1", cookie.MaxAge)
	}
	if !cookie.HttpOnly {
		t.Fatal("cleared cookie should be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cleared cookie SameSite = %v, want Lax", cookie.SameSite)
	}
}

func TestRecordLoginAttempt_MaxTrackedIPs(t *testing.T) {
	mgr := &SessionManager{
		loginAttempts: make(map[string][]time.Time),
	}

	// Fill to capacity with maxTrackedIPs distinct IPs.
	for i := 0; i < maxTrackedIPs; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		mgr.RecordLoginAttempt(ip)
	}

	if len(mgr.loginAttempts) != maxTrackedIPs {
		t.Fatalf("expected %d tracked IPs, got %d", maxTrackedIPs, len(mgr.loginAttempts))
	}

	// A new IP beyond the limit should be silently dropped.
	mgr.RecordLoginAttempt("192.168.99.99")
	if _, exists := mgr.loginAttempts["192.168.99.99"]; exists {
		t.Fatal("new IP should not be added when at maxTrackedIPs capacity")
	}
	if len(mgr.loginAttempts) != maxTrackedIPs {
		t.Fatalf("tracked IPs count should remain %d, got %d", maxTrackedIPs, len(mgr.loginAttempts))
	}

	// An existing IP should still be able to record attempts at capacity.
	existingIP := "10.0.0.0"
	before := len(mgr.loginAttempts[existingIP])
	mgr.RecordLoginAttempt(existingIP)
	after := len(mgr.loginAttempts[existingIP])
	if after != before+1 {
		t.Fatalf("existing IP should record attempt: expected %d attempts, got %d", before+1, after)
	}
}

func TestRecordLoginAttempt_Normal(t *testing.T) {
	mgr := &SessionManager{
		loginAttempts: make(map[string][]time.Time),
	}

	mgr.RecordLoginAttempt("10.0.0.1")
	mgr.RecordLoginAttempt("10.0.0.1")
	mgr.RecordLoginAttempt("10.0.0.2")

	if got := len(mgr.loginAttempts["10.0.0.1"]); got != 2 {
		t.Fatalf("expected 2 attempts for 10.0.0.1, got %d", got)
	}
	if got := len(mgr.loginAttempts["10.0.0.2"]); got != 1 {
		t.Fatalf("expected 1 attempt for 10.0.0.2, got %d", got)
	}
}

func TestAPIKeyFlashRoundTrip(t *testing.T) {
	mgr := &SessionManager{
		apiKeyFlashes: make(map[string]apiKeyFlash),
	}

	mgr.SetAPIKeyFlash("session-hash", "proj-1", "key-1", "secret-1")

	keyID, secret, ok := mgr.PopAPIKeyFlash("session-hash", "proj-1")
	if !ok {
		t.Fatal("expected flash to be present")
	}
	if keyID != "key-1" || secret != "secret-1" {
		t.Fatalf("unexpected flash values: keyID=%q secret=%q", keyID, secret)
	}

	_, _, ok = mgr.PopAPIKeyFlash("session-hash", "proj-1")
	if ok {
		t.Fatal("expected flash to be consumed after pop")
	}
}
