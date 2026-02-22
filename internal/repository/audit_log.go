package repository

import (
"context"
"encoding/json"
"time"
)

// AuditLogEntry records a mutation performed via the API or admin portal.
//
// Note: The audit_log table is created by migration 0004_audit_log
// (phase-4.3/audit-logging branch). This branch adds admin-specific
// audit logging that writes to the same table.
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
// Note: InsertAuditLog is covered by integration tests (PR #47).
func (r *PostgresRepository) InsertAuditLog(ctx context.Context, entry AuditLogEntry) error {
_, err := r.pool.Exec(ctx,
`INSERT INTO audit_log (project_id, api_key_id, admin_user_id, action, flag_key, details)
 VALUES ($1, $2, $3, $4, $5, $6)`,
entry.ProjectID, entry.APIKeyID, entry.AdminUserID, entry.Action, entry.FlagKey, entry.Details,
)
return err
}
