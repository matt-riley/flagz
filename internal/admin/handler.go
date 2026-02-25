package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/matt-riley/flagz/internal/middleware"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

type adminContextKey string

const sessionContextKey adminContextKey = "admin_session"
const adminUserContextKey adminContextKey = "admin_user"

const (
	adminAuditWriteTimeout = 2 * time.Second
	defaultProjectID       = "11111111-1111-1111-1111-111111111111"
)

type Handler struct {
	Repo          *repository.PostgresRepository
	Service       *service.Service
	SessionMgr    *SessionManager
	Templates     *TemplateManager
	AdminHostname string
	log           *slog.Logger
	mux           *http.ServeMux
}

type TemplateManager struct {
	// Wrapper to help with rendering
}

func NewHandler(repo *repository.PostgresRepository, svc *service.Service, sessionMgr *SessionManager, adminHostname string, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		Repo:          repo,
		Service:       svc,
		SessionMgr:    sessionMgr,
		AdminHostname: adminHostname,
		log:           log,
	}
	h.mux = h.buildMux()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("/login", h.handleLogin)
	mux.HandleFunc("/setup", h.handleSetup)
	mux.HandleFunc("/logout", h.handleLogout)

	// Protected routes
	mux.HandleFunc("/", h.requireAuth(h.handleDashboard))
	mux.HandleFunc("/projects", h.requireAuth(h.requireAdmin(h.handleProjects))) // Create project
	mux.HandleFunc("/projects/", h.requireAuth(h.handleProjectDetail))
	mux.HandleFunc("/api-keys/", h.requireAuth(h.handleAPIKeys))
	mux.HandleFunc("/api-keys/delete/", h.requireAuth(h.requireAdmin(h.handleDeleteAPIKey)))
	mux.HandleFunc("/audit-log/", h.requireAuth(h.handleAuditLog))

	// Static assets
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(content))))

	return mux
}

// requireAuth middleware ensures a valid session exists and validates
// CSRF tokens on state-changing requests.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("flagz_admin_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		session, err := h.SessionMgr.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// Validate CSRF token on state-changing requests
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			csrfToken := r.FormValue("csrf_token")
			if csrfToken == "" {
				csrfToken = r.Header.Get("X-CSRF-Token")
			}
			if subtle.ConstantTimeCompare([]byte(csrfToken), []byte(session.CSRFToken)) != 1 {
				http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, sessionContextKey, session)
		ctx = middleware.NewContextWithAdminUserID(ctx, session.AdminUserID)
		next(w, r.WithContext(ctx))
	}
}

// requireAdmin blocks write operations for viewer-role users.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		user, err := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if !isAdminRole(user.Role) {
			http.Error(w, "Forbidden: admin role required", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), adminUserContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

func isAdminRole(role string) bool {
	return role == "admin"
}

func canManageAPIKeys(method, role string) bool {
	if method == http.MethodPost {
		return isAdminRole(role)
	}

	return true
}

func canMutateFlags(method, role string) bool {
	if method == http.MethodPost || method == http.MethodDelete || method == http.MethodPut || method == http.MethodPatch {
		return isAdminRole(role)
	}

	return true
}

func (h *Handler) handleSetup(w http.ResponseWriter, r *http.Request) {
	// Check if admin user exists
	exists, err := h.Repo.HasAdminUsers(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.Method == "GET" {
		csrfToken := h.generateCSRFToken()
		isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
		http.SetCookie(w, &http.Cookie{
			Name:     "flagz_csrf",
			Value:    csrfToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   isSecure,
		})
		if err := Render(w, "setup.html", map[string]any{
			"CSRFToken": csrfToken,
		}); err != nil {
			h.log.Error("render error", "error", err)
		}
		return
	}

	if r.Method == "POST" {
		if !h.validateDoubleSubmitCSRF(r) {
			http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
			return
		}
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		confirm := r.FormValue("confirm_password")

		if len(username) < 3 || len(username) > 50 {
			if err := Render(w, "setup.html", map[string]any{"Error": "Username must be between 3 and 50 characters"}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}
		for _, c := range username {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
				if err := Render(w, "setup.html", map[string]any{"Error": "Username may only contain letters, digits, underscores, hyphens, and dots"}); err != nil {
					h.log.Error("render error", "error", err)
				}
				return
			}
		}

		if password != confirm {
			if err := Render(w, "setup.html", map[string]any{"Error": "Passwords do not match"}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}

		if len(password) < 12 {
			if err := Render(w, "setup.html", map[string]any{"Error": "Password must be at least 12 characters"}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}

		hash, err := HashPassword(password)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		user, err := h.Repo.CreateAdminUser(r.Context(), username, hash, "admin")
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			h.log.Error("failed to create admin user", "error", err)
			if err := Render(w, "setup.html", map[string]any{"Error": "Failed to create user"}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}

		h.logAudit(r.Context(), user.ID, "admin_setup", defaultProjectID, "", map[string]string{"username": username})

		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		csrfToken := h.generateCSRFToken()
		isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
		http.SetCookie(w, &http.Cookie{
			Name:     "flagz_csrf",
			Value:    csrfToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   isSecure,
		})
		if err := Render(w, "login.html", map[string]any{
			"CSRFToken": csrfToken,
		}); err != nil {
			h.log.Error("render error", "error", err)
		}
		return
	}

	if r.Method == "POST" {
		if !h.validateDoubleSubmitCSRF(r) {
			http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
			return
		}
		username := r.FormValue("username")
		password := r.FormValue("password")

		// Only trust proxy headers when the request comes from a
		// loopback or private address (i.e., a trusted reverse proxy).
		remoteAddr := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			remoteAddr = host
		}
		if ip := net.ParseIP(remoteAddr); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
			if xri := r.Header.Get("X-Real-IP"); xri != "" {
				remoteAddr = xri
			} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				first, _, _ := strings.Cut(xff, ",")
				remoteAddr = strings.TrimSpace(first)
			}
		}

		if allowed := h.SessionMgr.CheckLoginRateLimit(remoteAddr); !allowed {
			if err := Render(w, "login.html", map[string]any{"Error": "Too many attempts. Please try again later."}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}

		user, err := h.Repo.GetAdminUserByUsername(r.Context(), username)
		if err != nil {
			h.SessionMgr.RecordLoginAttempt(remoteAddr)
			// Don't reveal if user exists vs db error, generally
			// For admin portal, generic error is fine
			if err := Render(w, "login.html", map[string]any{"Error": "Invalid credentials"}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}

		match, err := VerifyPassword(password, user.PasswordHash)
		if err != nil || !match {
			h.SessionMgr.RecordLoginAttempt(remoteAddr)
			if err := Render(w, "login.html", map[string]any{"Error": "Invalid credentials"}); err != nil {
				h.log.Error("render error", "error", err)
			}
			return
		}

		token, err := h.SessionMgr.GenerateSession(r.Context(), user.ID)
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}
		h.SessionMgr.SetSessionCookie(w, token)

		h.logAudit(r.Context(), user.ID, "admin_login", defaultProjectID, "", nil)

		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		cookie, err := r.Cookie("flagz_admin_session")
		if err == nil {
			h.SessionMgr.InvalidateSession(r.Context(), cookie.Value)
		}
		h.SessionMgr.ClearSessionCookie(w)
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	user, err := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)
	if err != nil {
		if cookie, cerr := r.Cookie("flagz_admin_session"); cerr == nil {
			h.SessionMgr.InvalidateSession(r.Context(), cookie.Value)
		}
		h.SessionMgr.ClearSessionCookie(w)
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	projects, err := h.Repo.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "Failed to list projects", http.StatusInternalServerError)
		return
	}

	if err := Render(w, "dashboard.html", map[string]any{
		"User":      user,
		"Projects":  projects,
		"CSRFToken": session.CSRFToken,
	}); err != nil {
		h.log.Error("render error", "error", err)
	}
}

func (h *Handler) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		name := r.FormValue("name")
		desc := r.FormValue("description")

		p, err := h.Repo.CreateProject(r.Context(), name, desc)
		if err != nil {
			http.Error(w, "Failed to create project", http.StatusInternalServerError)
			return
		}

		h.logAudit(r.Context(), session.AdminUserID, "project_create", p.ID, "", map[string]string{"name": name})

		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (h *Handler) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// URL pattern: /projects/{id} or /projects/{id}/flags...
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/projects/"), "/")
	if len(pathParts) == 0 {
		http.NotFound(w, r)
		return
	}
	projectIDStr := pathParts[0]
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	project, err := h.Repo.GetProject(r.Context(), projectID.String())
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Fetch user for template
	user, err := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	// Handle sub-resources
	if len(pathParts) > 1 {
		if pathParts[1] == "flags" {
			h.handleFlags(w, r, &project, pathParts[2:])
			return
		}
	}

	// GET /projects/{id} -> Show detail
	flags, err := h.Repo.ListFlagsByProject(r.Context(), projectID.String())
	if err != nil {
		http.Error(w, "Failed to list flags", http.StatusInternalServerError)
		return
	}

	if err := Render(w, "project.html", map[string]any{
		"User":      user,
		"Project":   project,
		"Flags":     flags,
		"CSRFToken": session.CSRFToken,
	}); err != nil {
		h.log.Error("render error", "error", err)
	}
}

func (h *Handler) handleFlags(w http.ResponseWriter, r *http.Request, project *repository.Project, subPath []string) {
	session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.Method != http.MethodGet {
		user, err := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)
		if err != nil {
			http.Error(w, "User not found", http.StatusUnauthorized)
			return
		}
		if !canMutateFlags(r.Method, user.Role) {
			http.Error(w, "Forbidden: admin role required", http.StatusForbidden)
			return
		}
	}

	// POST /projects/{id}/flags
	if len(subPath) == 0 && r.Method == "POST" {
		key := r.FormValue("key")
		desc := r.FormValue("description")
		enabled := r.FormValue("enabled") == "on"

		flag := repository.Flag{
			ProjectID:   project.ID,
			Key:         key,
			Description: desc,
			Enabled:     enabled,
			Variants:    []byte("null"), // Default null variants
			Rules:       []byte("[]"),   // Default empty rules
		}

		_, err := h.Service.CreateFlag(r.Context(), flag)
		if err != nil {
			http.Error(w, "Failed to create flag: "+err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/projects/%s", project.ID), http.StatusFound)
		return
	}

	if len(subPath) < 1 {
		http.NotFound(w, r)
		return
	}
	flagKey := subPath[0]

	// POST /projects/{id}/flags/{key}/toggle
	if len(subPath) == 2 && subPath[1] == "toggle" && r.Method == "POST" {
		repoFlag, err := h.Repo.GetFlag(r.Context(), project.ID, flagKey)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		repoFlag.Enabled = !repoFlag.Enabled
		_, err = h.Service.UpdateFlag(r.Context(), repoFlag)
		if err != nil {
			http.Error(w, "Failed to update flag", http.StatusInternalServerError)
			return
		}

		// Render just the button if HTMX request
		if r.Header.Get("HX-Request") == "true" {
			colorClass := "bg-red-100 text-red-800"
			text := "Disabled"
			if repoFlag.Enabled {
				colorClass = "bg-green-100 text-green-800"
				text = "Enabled"
			}

			tmpl := template.Must(template.New("toggle").Parse(
				`<button hx-post="/projects/{{.ProjectID}}/flags/{{.FlagKey}}/toggle" ` +
					`hx-vals='{"csrf_token": "{{.CSRFToken}}"}' hx-target="this" hx-swap="outerHTML" ` +
					`class="{{.ColorClass}} px-2 inline-flex text-xs leading-5 font-semibold rounded-full cursor-pointer">{{.Text}}</button>`))

			w.Header().Set("Content-Type", "text/html")
			tmpl.Execute(w, map[string]string{
				"ProjectID":  project.ID,
				"FlagKey":    flagKey,
				"CSRFToken":  r.FormValue("csrf_token"),
				"ColorClass": colorClass,
				"Text":       text,
			})
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/projects/%s", project.ID), http.StatusFound)
		return
	}

	// DELETE /projects/{id}/flags/{key}
	if len(subPath) == 1 && r.Method == "DELETE" {
		if err := h.Service.DeleteFlag(r.Context(), project.ID, flagKey); err != nil {
			http.Error(w, "Failed to delete flag", http.StatusInternalServerError)
			return
		}

		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusOK) // Empty response removes the element
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/projects/%s", project.ID), http.StatusFound)
		return
	}
}

func (h *Handler) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	projectID := strings.TrimPrefix(r.URL.Path, "/api-keys/")
	if projectID == "" {
		http.NotFound(w, r)
		return
	}

	project, err := h.Repo.GetProject(r.Context(), projectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user, err := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	if !canManageAPIKeys(r.Method, user.Role) {
		http.Error(w, "Forbidden: admin role required", http.StatusForbidden)
		return
	}

	if r.Method == "POST" {
		keyID, rawSecret, createErr := h.Repo.CreateAPIKeyForProject(r.Context(), projectID)
		if createErr != nil {
			http.Error(w, "Failed to create API key", http.StatusInternalServerError)
			return
		}
		h.logAudit(r.Context(), session.AdminUserID, "api_key_create", projectID, "", map[string]string{"api_key_id": keyID})

		keys, listErr := h.Repo.ListAPIKeysForProject(r.Context(), projectID)
		if listErr != nil {
			h.log.Error("failed to list API keys", "project_id", projectID, "error", listErr)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if renderErr := Render(w, "api_keys.html", map[string]any{
			"User":      user,
			"Project":   project,
			"APIKeys":   keys,
			"NewKeyID":  keyID,
			"NewSecret": rawSecret,
			"CSRFToken": session.CSRFToken,
		}); renderErr != nil {
			h.log.Error("render error", "error", renderErr)
		}
		return
	}

	keys, err := h.Repo.ListAPIKeysForProject(r.Context(), projectID)
	if err != nil {
		http.Error(w, "Failed to list API keys", http.StatusInternalServerError)
		return
	}

	if renderErr := Render(w, "api_keys.html", map[string]any{
		"User":      user,
		"Project":   project,
		"APIKeys":   keys,
		"CSRFToken": session.CSRFToken,
	}); renderErr != nil {
		h.log.Error("render error", "error", renderErr)
	}
}

func (h *Handler) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projectID := strings.TrimPrefix(r.URL.Path, "/api-keys/delete/")
	if projectID == "" {
		http.NotFound(w, r)
		return
	}

	keyID := r.FormValue("key_id")
	if keyID == "" {
		http.Error(w, "Missing key_id", http.StatusBadRequest)
		return
	}

	if err := h.Repo.DeleteAPIKeyByID(r.Context(), projectID, keyID); err != nil {
		http.Error(w, "Failed to delete API key", http.StatusInternalServerError)
		return
	}
	adminUser := r.Context().Value(adminUserContextKey).(repository.AdminUser)
	h.logAudit(r.Context(), adminUser.ID, "api_key_delete", projectID, "", map[string]string{"api_key_id": keyID})

	http.Redirect(w, r, fmt.Sprintf("/api-keys/%s", projectID), http.StatusFound)
}

func (h *Handler) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := r.Context().Value(sessionContextKey).(repository.AdminSession)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	projectID := strings.TrimPrefix(r.URL.Path, "/audit-log/")
	if projectID == "" {
		http.NotFound(w, r)
		return
	}

	project, err := h.Repo.GetProject(r.Context(), projectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user, err := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	entries, err := h.Repo.ListAuditLogForProject(r.Context(), projectID, 100)
	if err != nil {
		http.Error(w, "Failed to load audit log", http.StatusInternalServerError)
		return
	}

	if renderErr := Render(w, "audit_log.html", map[string]any{
		"User":      user,
		"Project":   project,
		"Entries":   entries,
		"CSRFToken": session.CSRFToken,
	}); renderErr != nil {
		h.log.Error("render error", "error", renderErr)
	}
}

func (h *Handler) generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate CSRF token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// validateDoubleSubmitCSRF checks that the CSRF form value matches the
// flagz_csrf cookie, implementing the double-submit cookie pattern for
// pre-authentication forms (login, setup).
func (h *Handler) validateDoubleSubmitCSRF(r *http.Request) bool {
	cookie, err := r.Cookie("flagz_csrf")
	if err != nil || cookie.Value == "" {
		return false
	}
	formToken := r.FormValue("csrf_token")
	if formToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(formToken)) == 1
}

// logAudit writes an audit log entry on a best-effort basis.
// Failures are logged but never propagated to the caller.
func (h *Handler) logAudit(ctx context.Context, adminUserID, action, projectID, flagKey string, details any) {
	entry, err := buildAuditEntry(adminUserID, action, projectID, flagKey, details)
	if err != nil {
		h.log.Error("audit log: marshal details",
			"error", err,
			"action", action,
			"project_id", projectID,
			"flag_key", flagKey,
			"admin_user_id", adminUserID,
		)
		return
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), adminAuditWriteTimeout)
	defer cancel()

	if err := h.Repo.InsertAuditLog(writeCtx, entry); err != nil {
		h.log.Error("audit log write failed",
			"error", err,
			"action", action,
			"project_id", projectID,
			"flag_key", flagKey,
			"admin_user_id", adminUserID,
		)
	}
}

// buildAuditEntry constructs an AuditLogEntry, marshalling the details to JSON.
func buildAuditEntry(adminUserID, action, projectID, flagKey string, details any) (repository.AuditLogEntry, error) {
	var detailsJSON json.RawMessage
	if details != nil {
		b, err := json.Marshal(details)
		if err != nil {
			return repository.AuditLogEntry{}, err
		}
		detailsJSON = b
	}
	return repository.AuditLogEntry{
		ProjectID:   projectID,
		APIKeyID:    "",
		AdminUserID: adminUserID,
		Action:      action,
		FlagKey:     flagKey,
		Details:     detailsJSON,
	}, nil
}
