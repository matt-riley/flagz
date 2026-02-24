package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/matt-riley/flagz/internal/repository"
)

func TestRenderAPIKeysTemplate(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "api_keys.html", map[string]any{
		"User":      repository.AdminUser{Username: "admin", Role: "admin"},
		"Project":   repository.Project{ID: "proj-1", Name: "Test Project"},
		"APIKeys":   []repository.APIKeyMeta{{ID: "key-1", CreatedAt: time.Now()}},
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "API Keys") {
		t.Error("expected 'API Keys' in output")
	}
	if !strings.Contains(out, "key-1") {
		t.Error("expected key ID in output")
	}
	if !strings.Contains(out, "Create API Key") {
		t.Error("admin should see Create button")
	}
}

func TestRenderAPIKeysTemplate_ViewerRole(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "api_keys.html", map[string]any{
		"User":      repository.AdminUser{Username: "viewer", Role: "viewer"},
		"Project":   repository.Project{ID: "proj-1", Name: "Test Project"},
		"APIKeys":   []repository.APIKeyMeta{},
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Create API Key") {
		t.Error("viewer should NOT see Create button")
	}
}

func TestRenderAPIKeysTemplate_NewSecret(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "api_keys.html", map[string]any{
		"User":      repository.AdminUser{Username: "admin", Role: "admin"},
		"Project":   repository.Project{ID: "proj-1", Name: "Test Project"},
		"APIKeys":   []repository.APIKeyMeta{},
		"NewKeyID":  "abc123",
		"NewSecret": "secret456",
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "abc123.secret456") {
		t.Error("expected full token in output")
	}
	if !strings.Contains(out, "will not be shown again") {
		t.Error("expected warning about secret visibility")
	}
}

func TestRenderAuditLogTemplate(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "audit_log.html", map[string]any{
		"User":    repository.AdminUser{Username: "admin", Role: "admin"},
		"Project": repository.Project{ID: "proj-1", Name: "Test Project"},
		"Entries": []repository.AuditLogEntry{
			{ID: 1, FlagKey: "dark-mode", Action: "flag_update", CreatedAt: time.Now()},
			{ID: 2, FlagKey: "", Action: "admin_login", CreatedAt: time.Now()},
		},
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Audit Log") {
		t.Error("expected 'Audit Log' in output")
	}
	if !strings.Contains(out, "dark-mode") {
		t.Error("expected flag key in output")
	}
	if !strings.Contains(out, "flag_update") {
		t.Error("expected action in output")
	}
}

func TestRenderAuditLogTemplate_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "audit_log.html", map[string]any{
		"User":      repository.AdminUser{Username: "admin", Role: "admin"},
		"Project":   repository.Project{ID: "proj-1", Name: "Test Project"},
		"Entries":   []repository.AuditLogEntry{},
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(buf.String(), "No audit log entries found") {
		t.Error("expected empty state message")
	}
}

func TestRenderProjectTemplate_ViewerRole(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "project.html", map[string]any{
		"User":      repository.AdminUser{Username: "viewer", Role: "viewer"},
		"Project":   repository.Project{ID: "proj-1", Name: "Test Project"},
		"Flags":     []repository.Flag{{Key: "dark-mode", Enabled: true}},
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "New Flag") {
		t.Error("viewer should not see New Flag control")
	}
	if strings.Contains(out, "hx-delete=\"/projects/proj-1/flags/dark-mode\"") {
		t.Error("viewer should not see delete control")
	}
	if strings.Contains(out, "hx-post=\"/projects/proj-1/flags/dark-mode/toggle\"") {
		t.Error("viewer should not see toggle mutation control")
	}
}

func TestIsAdminRole(t *testing.T) {
	tests := []struct {
		name string
		role string
		want bool
	}{
		{name: "admin role", role: "admin", want: true},
		{name: "viewer role", role: "viewer", want: false},
		{name: "empty role", role: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAdminRole(tt.role); got != tt.want {
				t.Fatalf("isAdminRole(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestCanManageAPIKeys(t *testing.T) {
	tests := []struct {
		name   string
		method string
		role   string
		want   bool
	}{
		{name: "viewer can read", method: "GET", role: "viewer", want: true},
		{name: "viewer cannot create", method: "POST", role: "viewer", want: false},
		{name: "admin can create", method: "POST", role: "admin", want: true},
		{name: "admin can read", method: "GET", role: "admin", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canManageAPIKeys(tt.method, tt.role); got != tt.want {
				t.Fatalf("canManageAPIKeys(%q,%q) = %v, want %v", tt.method, tt.role, got, tt.want)
			}
		})
	}
}

func TestRequireAdmin_MissingSessionRedirects(t *testing.T) {
	h := &Handler{}
	nextCalled := false
	handler := h.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if nextCalled {
		t.Fatal("expected next handler not to be called")
	}
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
}

func TestHandleDeleteAPIKey_MissingProjectID(t *testing.T) {
	h := &Handler{}
	form := url.Values{"key_id": {"key-1"}}
	req := httptest.NewRequest(http.MethodPost, "/api-keys/delete/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.handleDeleteAPIKey(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteAPIKey_MissingKeyID(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api-keys/delete/proj-1", nil)
	rr := httptest.NewRecorder()

	h.handleDeleteAPIKey(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
