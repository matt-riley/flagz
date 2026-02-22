package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

type Handler struct {
	Repo         *repository.PostgresRepository
	Service      *service.Service
	SessionMgr   *SessionManager
	Templates    *TemplateManager
	AdminHostname string
}

type TemplateManager struct {
	// Wrapper to help with rendering
}

func NewHandler(repo *repository.PostgresRepository, svc *service.Service, sessionMgr *SessionManager, adminHostname string) *Handler {
	return &Handler{
		Repo:          repo,
		Service:       svc,
		SessionMgr:    sessionMgr,
		AdminHostname: adminHostname,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	mux.ServeHTTP(w, r)
}

// requireAuth middleware ensures a valid session exists
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

		// Inject session into context if needed, or just pass user info to template
		ctx := r.Context()
		// (Optional) add user to context
		// For now we just proceed, handleDashboard et al will re-fetch or we should put in context
		// Let's put the session in context to avoid re-fetching cookie
		ctx = context.WithValue(ctx, "admin_session", session)
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
		csrfToken := h.generateCSRFToken() // In a real app, bind this to a pre-session cookie
		Render(w, "setup.html", map[string]any{
			"CSRFToken": csrfToken,
		})
		return
	}

	if r.Method == "POST" {
		username := r.FormValue("username")
		password := r.FormValue("password")
		confirm := r.FormValue("confirm_password")

		if password != confirm {
			Render(w, "setup.html", map[string]any{"Error": "Passwords do not match"})
			return
		}

		hash, err := HashPassword(password)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}

		if _, err := h.Repo.CreateAdminUser(r.Context(), username, hash); err != nil {
			log.Printf("Failed to create admin user: %v", err)
			Render(w, "setup.html", map[string]any{"Error": "Failed to create user"})
			return
		}

		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		Render(w, "login.html", map[string]any{
			"CSRFToken": "login-csrf", // TODO: Implement proper CSRF
		})
		return
	}

	if r.Method == "POST" {
		username := r.FormValue("username")
		password := r.FormValue("password")

		remoteAddr := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			remoteAddr = host
		}
		
		if allowed := h.SessionMgr.CheckLoginRateLimit(remoteAddr); !allowed {
			Render(w, "login.html", map[string]any{"Error": "Too many attempts. Please try again later."})
			return
		}

		user, err := h.Repo.GetAdminUserByUsername(r.Context(), username)
		if err != nil {
			h.SessionMgr.RecordLoginAttempt(remoteAddr)
			// Don't reveal if user exists vs db error, generally
			// For admin portal, generic error is fine
			Render(w, "login.html", map[string]any{"Error": "Invalid credentials"})
			return
		}

		match, err := VerifyPassword(password, user.PasswordHash)
		if err != nil || !match {
			h.SessionMgr.RecordLoginAttempt(remoteAddr)
			Render(w, "login.html", map[string]any{"Error": "Invalid credentials"})
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
	session, ok := r.Context().Value("admin_session").(repository.AdminSession)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	user, _ := h.Repo.GetAdminUserByID(r.Context(), session.AdminUserID)

	projects, err := h.Repo.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "Failed to list projects", http.StatusInternalServerError)
		return
	}

	Render(w, "dashboard.html", map[string]any{
		"User":      user,
		"Projects":  projects,
		"CSRFToken": session.CSRFToken,
	})
}

func (h *Handler) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		name := r.FormValue("name")
		desc := r.FormValue("description")

		if _, err := h.Repo.CreateProject(r.Context(), name, desc); err != nil {
			// TODO: Better error handling
			http.Error(w, "Failed to create project: "+err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (h *Handler) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	session, ok := r.Context().Value("admin_session").(repository.AdminSession)
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
		// If user not found (deleted?), invalidate session?
		// For now just log and continue or error
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

	Render(w, "project.html", map[string]any{
		"User":      user,
		"Project":   project,
		"Flags":     flags,
		"CSRFToken": session.CSRFToken,
	})
}

func (h *Handler) handleFlags(w http.ResponseWriter, r *http.Request, project *repository.Project, subPath []string) {
	// POST /projects/{id}/flags
	if len(subPath) == 0 && r.Method == "POST" {
		key := r.FormValue("key")
		desc := r.FormValue("description")
		enabled := r.FormValue("enabled") == "on"

		// Use Service to create flag (handles cache update and event publishing)
		// We need to map inputs to Service.CreateFlag inputs
		// Service expects: ctx, flag repository.Flag
		// CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
		
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
		// Get flag via Repo to get raw bytes (Service returns parsed core.Flag)
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

		// Render just the row if HTMX request
		if r.Header.Get("HX-Request") == "true" {
			// This is a bit hacky, normally you'd have a partial template for the row
			// For now, let's just re-render the button or the whole row?
			// The button is easier to construct manually for this MVP
			colorClass := "bg-red-100 text-red-800"
			text := "Disabled"
			if repoFlag.Enabled {
				colorClass = "bg-green-100 text-green-800"
				text = "Enabled"
			}
			
			html := fmt.Sprintf(`<button hx-post="/projects/%s/flags/%s/toggle" hx-vals='{"csrf_token": "%s"}' hx-target="#flag-row-%s" hx-swap="outerHTML" class="%s px-2 inline-flex text-xs leading-5 font-semibold rounded-full cursor-pointer">%s</button>`,
				project.ID, flagKey, r.FormValue("csrf_token"), flagKey, colorClass, text)
			
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
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
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Helper to convert repository.Flag to core.Flag if needed
func repositoryFlagToCore(f *repository.Flag) *core.Flag {
	// Re-implement if needed, or import from service if exposed
	return &core.Flag{
		Key:         f.Key,
		// Description not in core.Flag? Let's check.
		// Description: f.Description,
		Disabled:    !f.Enabled, // NOTE: Inversion
		// Variants/Rules parsing omitted for brevity
	}
}
