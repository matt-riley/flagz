package repository

import (
	"context"
	"fmt"
)

// ListFlagsByProject returns all flags for a specific project.
func (r *PostgresRepository) ListFlagsByProject(ctx context.Context, projectID string) ([]Flag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT project_id, key, description, enabled, variants, rules, created_at, updated_at
		FROM flags
		WHERE project_id = $1
		ORDER BY key
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list flags by project: %w", err)
	}
	defer rows.Close()

	flags := make([]Flag, 0)
	for rows.Next() {
		var flag Flag
		if err := rows.Scan(
			&flag.ProjectID,
			&flag.Key,
			&flag.Description,
			&flag.Enabled,
			&flag.Variants,
			&flag.Rules,
			&flag.CreatedAt,
			&flag.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan flag: %w", err)
		}

		flags = append(flags, flag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list flags rows: %w", err)
	}

	return flags, nil
}
