package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/matt-riley/flagz/internal/repository"
)

const (
	sessionCookieName  = "flagz_admin_session"
	sessionDuration    = 24 * time.Hour
	csrfTokenLength    = 32
	sessionTokenLength = 32
	maxLoginAttempts   = 5
	loginWindow        = 15 * time.Minute
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidCSRF  = errors.New("invalid CSRF token")
)

type SessionManager struct {
	repo          *repository.PostgresRepository
	sessionSecret []byte
	// Simple in-memory rate limiter for login attempts
	loginAttempts map[string][]time.Time
	mu            sync.Mutex
}

func NewSessionManager(repo *repository.PostgresRepository, sessionSecret string) *SessionManager {
	mgr := &SessionManager{
		repo:          repo,
		sessionSecret: []byte(sessionSecret),
		loginAttempts: make(map[string][]time.Time),
	}
	// Periodically clean up old rate limit entries to prevent unbounded memory growth
	// and purge expired sessions from the database.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			mgr.mu.Lock()
			now := time.Now()
			for ip, attempts := range mgr.loginAttempts {
				if len(attempts) == 0 || now.Sub(attempts[len(attempts)-1]) > loginWindow {
					delete(mgr.loginAttempts, ip)
				}
			}
			mgr.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = repo.DeleteExpiredAdminSessions(ctx)
			cancel()
		}
	}()
	return mgr
}

// GenerateSession creates a new session for the user, returning the raw token to be set in the cookie.
func (m *SessionManager) GenerateSession(ctx context.Context, userID string) (string, error) {
	// Generate raw session token
	tokenBytes := make([]byte, sessionTokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(tokenBytes)

	// Hash token for storage
	idHash := m.hashToken(rawToken)

	// Generate CSRF token
	csrfBytes := make([]byte, csrfTokenLength)
	if _, err := rand.Read(csrfBytes); err != nil {
		return "", fmt.Errorf("generate csrf token: %w", err)
	}
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfBytes)

	session := repository.AdminSession{
		IDHash:      idHash,
		AdminUserID: userID,
		CSRFToken:   csrfToken,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(sessionDuration),
	}

	if err := m.repo.CreateAdminSession(ctx, session); err != nil {
		return "", err
	}

	return rawToken, nil
}

// ValidateSession checks the cookie token against the DB and returns the session if valid.
func (m *SessionManager) ValidateSession(ctx context.Context, rawToken string) (repository.AdminSession, error) {
	if rawToken == "" {
		return repository.AdminSession{}, ErrUnauthorized
	}

	idHash := m.hashToken(rawToken)
	session, err := m.repo.GetAdminSession(ctx, idHash)
	if err != nil {
		return repository.AdminSession{}, ErrUnauthorized
	}

	if time.Now().After(session.ExpiresAt) {
		_ = m.repo.DeleteAdminSession(ctx, idHash)
		return repository.AdminSession{}, ErrUnauthorized
	}

	return session, nil
}

// InvalidateSession removes the session from the DB.
func (m *SessionManager) InvalidateSession(ctx context.Context, rawToken string) error {
	idHash := m.hashToken(rawToken)
	return m.repo.DeleteAdminSession(ctx, idHash)
}

// SetSessionCookie writes the session cookie.
func (m *SessionManager) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		// SameSite=Lax is safer for navigation than Strict, which can break links from external sites
		SameSite: http.SameSiteLaxMode,
		// Secure is omitted to allow plain HTTP over Tailscale (WireGuard encryption)
		// Adding Secure would break the admin portal unless TLS is explicitly configured.
		Secure:  false,
		Expires: time.Now().Add(sessionDuration),
	})
}

// ClearSessionCookie deletes the session cookie.
func (m *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// CheckLoginRateLimit returns true if the IP is allowed to attempt login.
func (m *SessionManager) CheckLoginRateLimit(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	attempts, ok := m.loginAttempts[ip]
	if !ok {
		return true
	}

	// Filter old attempts
	validAttempts := make([]time.Time, 0, len(attempts))
	for _, t := range attempts {
		if now.Sub(t) < loginWindow {
			validAttempts = append(validAttempts, t)
		}
	}
	m.loginAttempts[ip] = validAttempts

	return len(validAttempts) < maxLoginAttempts
}

// RecordLoginAttempt adds a failed login attempt for the IP.
func (m *SessionManager) RecordLoginAttempt(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.loginAttempts[ip] = append(m.loginAttempts[ip], time.Now())
}

// hashToken computes an HMAC-SHA256 of the token using the session secret,
// preventing offline token forgery if the database is compromised.
func (m *SessionManager) hashToken(token string) string {
	mac := hmac.New(sha256.New, m.sessionSecret)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}
