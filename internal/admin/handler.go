package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

type adminContextKey string

const sessionContextKey adminContextKey = "admin_session"

type Handler struct {
	Repo          *repository.PostgresRepository
	Service       *service.Service
	SessionMgr    *SessionManager
	Templates     *TemplateManager
	AdminHostname string
	mux           *http.ServeMux
}

type TemplateManager struct {
	// Wrapper to help with rendering
}

func NewHandler(repo *repository.PostgresRepository, svc *service.Service, sessionMgr *SessionManager, adminHostname string) *Handler {
	h := &Handler{
		Repo:          repo,
		Service:       svc,
		SessionMgr:    sessionMgr,
		AdminHostname: adminHostname,
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
	mux.HandleFunc("/projects", h.requireAuth(h.handleProjects)) // Create project
	mux.HandleFunc("/projects/", h.requireAuth(h.handleProjectDetail))

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
		next(w, r.WithContext(ctx))
	}
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
			log.Printf("render error: %v", err)
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
				log.Printf("render error: %v", err)
			}
			return
		}
		for _, c := range username {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
				if err := Render(w, "setup.html", map[string]any{"Error": "Username may only contain letters, digits, underscores, hyphens, and dots"}); err != nil {
					log.Printf("render error: %v", err)
				}
				return
			}
		}

		if password != confirm {
			if err := Render(w, "setup.html", map[string]any{"Error": "Passwords do not match"}); err != nil {
				log.Printf("render error: %v", err)
			}
			return
		}

		if len(password) < 12 {
			if err := Render(w, "setup.html", map[string]any{"Error": "Password must be at least 12 characters"}); err != nil {
				log.Printf("render error: %v", err)
			}
			return
		}

		hash, err := HashPassword(password)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		if _, err := h.Repo.CreateAdminUser(r.Context(), username, hash); err != nil {
			if strings.Contains(err.Error(), "duplicate key") {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			log.Printf("Failed to create admin user: %v", err)
			if err := Render(w, "setup.html", map[string]any{"Error": "Failed to create user"}); err != nil {
				log.Printf("render error: %v", err)
			}
			return
		}

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
			log.Printf("render error: %v", err)
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

		remoteAddr := r.Header.Get("X-Real-IP")
		if remoteAddr == "" {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				remoteAddr, _, _ = strings.Cut(xff, ",")
				remoteAddr = strings.TrimSpace(remoteAddr)
			}
		}
		if remoteAddr == "" {
			remoteAddr = r.RemoteAddr
			if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
				remoteAddr = host
			}
		}
		
		if allowed := h.SessionMgr.CheckLoginRateLimit(remoteAddr); !allowed {
			if err := Render(w, "login.html", map[string]any{"Error": "Too many attempts. Please try again later."}); err != nil {
				log.Printf("render error: %v", err)
			}
			return
		}

		user, err := h.Repo.GetAdminUserByUsername(r.Context(), username)
		if err != nil {
			h.SessionMgr.RecordLoginAttempt(remoteAddr)
			// Don't reveal if user exists vs db error, generally
			// For admin portal, generic error is fine
			if err := Render(w, "login.html", map[string]any{"Error": "Invalid credentials"}); err != nil {
				log.Printf("render error: %v", err)
			}
			return
		}

		match, err := VerifyPassword(password, user.PasswordHash)
		if err != nil || !match {
			h.SessionMgr.RecordLoginAttempt(remoteAddr)
			if err := Render(w, "login.html", map[string]any{"Error": "Invalid credentials"}); err != nil {
				log.Printf("render error: %v", err)
			}
			return
		}

		token, err := h.SessionMgr.GenerateSession(r.Context(), user.ID)
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}
		h.SessionMgr.SetSessionCookie(w, token)

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
		log.Printf("render error: %v", err)
	}
}

func (h *Handler) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		name := r.FormValue("name")
		desc := r.FormValue("description")

		if _, err := h.Repo.CreateProject(r.Context(), name, desc); err != nil {
			http.Error(w, "Failed to create project", http.StatusInternalServerError)
			return
		}

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
		log.Printf("render error: %v", err)
	}
}

func (h *Handler) handleFlags(w http.ResponseWriter, r *http.Request, project *repository.Project, subPath []string) {
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
