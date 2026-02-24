package repository

import (
	"context"
)

// ListAuditLogForProject returns recent audit log entries for a project,
// ordered newest-first.
func (r *PostgresRepository) ListAuditLogForProject(ctx context.Context, projectID string, limit int) ([]AuditLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	return r.ListAuditLog(ctx, projectID, limit, 0)
}
