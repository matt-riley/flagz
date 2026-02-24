package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// AuditLogEntry records a mutation performed via the API or admin portal.
type AuditLogEntry struct {
	ID          int64           `json:"id"`
	ProjectID   string          `json:"project_id"`
	APIKeyID    string          `json:"api_key_id,omitempty"`
	AdminUserID string          `json:"admin_user_id,omitempty"`
	Action      string          `json:"action"`
	FlagKey     string          `json:"flag_key,omitempty"`
	Details     json.RawMessage `json:"details,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// InsertAuditLog writes a single audit log entry.
func (r *PostgresRepository) InsertAuditLog(ctx context.Context, entry AuditLogEntry) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO audit_log (project_id, api_key_id, admin_user_id, action, flag_key, details)
 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ProjectID, entry.APIKeyID, entry.AdminUserID, entry.Action, entry.FlagKey, entry.Details,
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}

	return nil
}
