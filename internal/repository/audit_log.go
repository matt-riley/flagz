package repository

import (
"context"
"fmt"
)

// ListAuditLogForProject returns recent flag events for a project, ordered
// newest-first. These events serve as the audit trail visible in the admin UI.
func (r *PostgresRepository) ListAuditLogForProject(ctx context.Context, projectID string, limit int) ([]FlagEvent, error) {
if limit <= 0 {
limit = 50
}
rows, err := r.pool.Query(ctx, `
SELECT event_id, project_id, flag_key, event_type, payload, created_at
FROM flag_events
WHERE project_id = $1
ORDER BY event_id DESC
LIMIT $2
`, projectID, limit)
if err != nil {
return nil, fmt.Errorf("list audit log: %w", err)
}
defer rows.Close()

entries := make([]FlagEvent, 0)
for rows.Next() {
var e FlagEvent
if err := rows.Scan(&e.EventID, &e.ProjectID, &e.FlagKey, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
return nil, fmt.Errorf("scan audit log entry: %w", err)
}
entries = append(entries, e)
}
if err := rows.Err(); err != nil {
return nil, fmt.Errorf("audit log rows: %w", err)
}
return entries, nil
}
