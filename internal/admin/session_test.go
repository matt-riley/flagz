package admin

import (
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
	if cookie.Value != "" {
		t.Fatalf("cleared cookie should have empty value, got %q", cookie.Value)
	}
	if cookie.MaxAge != -1 {
		t.Fatalf("cleared cookie MaxAge = %d, want -1", cookie.MaxAge)
	}
}
