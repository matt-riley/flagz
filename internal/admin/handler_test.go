package admin

import (
	"bytes"
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
		"Entries": []repository.FlagEvent{
			{EventID: 1, FlagKey: "dark-mode", EventType: "created", CreatedAt: time.Now()},
			{EventID: 2, FlagKey: "dark-mode", EventType: "updated", CreatedAt: time.Now()},
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
	if !strings.Contains(out, "created") {
		t.Error("expected event type in output")
	}
}

func TestRenderAuditLogTemplate_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "audit_log.html", map[string]any{
		"User":      repository.AdminUser{Username: "admin", Role: "admin"},
		"Project":   repository.Project{ID: "proj-1", Name: "Test Project"},
		"Entries":   []repository.FlagEvent{},
		"CSRFToken": "token123",
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(buf.String(), "No audit log entries found") {
		t.Error("expected empty state message")
	}
}
