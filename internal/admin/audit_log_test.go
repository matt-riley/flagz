package admin

import (
	"encoding/json"
	"testing"

	"github.com/matt-riley/flagz/internal/repository"
)

func TestBuildAuditEntry_NilDetails(t *testing.T) {
	entry, err := buildAuditEntry("user-1", "admin_login", "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.AdminUserID != "user-1" {
		t.Errorf("admin_user_id: got %q, want %q", entry.AdminUserID, "user-1")
	}
	if entry.Action != "admin_login" {
		t.Errorf("action: got %q, want %q", entry.Action, "admin_login")
	}
	if entry.APIKeyID != "" {
		t.Errorf("api_key_id: got %q, want empty", entry.APIKeyID)
	}
	if entry.Details != nil {
		t.Errorf("details: got %s, want nil", entry.Details)
	}
}

func TestBuildAuditEntry_MapDetails(t *testing.T) {
	entry, err := buildAuditEntry("user-2", "flag_toggle", "proj-1", "my-flag", map[string]any{"enabled": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.ProjectID != "proj-1" {
		t.Errorf("project_id: got %q, want %q", entry.ProjectID, "proj-1")
	}
	if entry.FlagKey != "my-flag" {
		t.Errorf("flag_key: got %q, want %q", entry.FlagKey, "my-flag")
	}
	if entry.Action != "flag_toggle" {
		t.Errorf("action: got %q, want %q", entry.Action, "flag_toggle")
	}
	var details map[string]any
	if err := json.Unmarshal(entry.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if enabled, ok := details["enabled"].(bool); !ok || !enabled {
		t.Errorf("details.enabled: got %v, want true", details["enabled"])
	}
}

func TestBuildAuditEntry_UnmarshalableDetails(t *testing.T) {
	_, err := buildAuditEntry("user-3", "bad_action", "", "", make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable details")
	}
}

func TestBuildAuditEntry_ProjectCreate(t *testing.T) {
	entry, err := buildAuditEntry("user-4", "project_create", "proj-99", "", map[string]string{"name": "My Project"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Action != "project_create" {
		t.Errorf("action: got %q, want %q", entry.Action, "project_create")
	}
	if entry.ProjectID != "proj-99" {
		t.Errorf("project_id: got %q, want %q", entry.ProjectID, "proj-99")
	}
	if entry.FlagKey != "" {
		t.Errorf("flag_key: got %q, want empty", entry.FlagKey)
	}
}

func TestBuildAuditEntry_AdminSetup(t *testing.T) {
	entry, err := buildAuditEntry("user-5", "admin_setup", defaultProjectID, "", map[string]string{"username": "admin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Action != "admin_setup" {
		t.Errorf("action: got %q, want %q", entry.Action, "admin_setup")
	}
	if entry.AdminUserID != "user-5" {
		t.Errorf("admin_user_id: got %q, want %q", entry.AdminUserID, "user-5")
	}
	var details map[string]string
	if err := json.Unmarshal(entry.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["username"] != "admin" {
		t.Errorf("details.username: got %q, want %q", details["username"], "admin")
	}
}

func TestAuditLogEntry_JSONRoundTrip(t *testing.T) {
	entry := repository.AuditLogEntry{
		ID:          1,
		ProjectID:   "proj-1",
		APIKeyID:    "",
		AdminUserID: "user-1",
		Action:      "flag_toggle",
		FlagKey:     "dark-mode",
		Details:     json.RawMessage(`{"enabled":true}`),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got repository.AuditLogEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Action != entry.Action {
		t.Errorf("action: got %q, want %q", got.Action, entry.Action)
	}
	if got.FlagKey != entry.FlagKey {
		t.Errorf("flag_key: got %q, want %q", got.FlagKey, entry.FlagKey)
	}
	if got.AdminUserID != entry.AdminUserID {
		t.Errorf("admin_user_id: got %q, want %q", got.AdminUserID, entry.AdminUserID)
	}
	if string(got.Details) != string(entry.Details) {
		t.Errorf("details: got %s, want %s", got.Details, entry.Details)
	}
}
