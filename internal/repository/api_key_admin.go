package repository

import "context"

// CreateAPIKeyForProject creates an API key for the given project.
func (r *PostgresRepository) CreateAPIKeyForProject(ctx context.Context, projectID string) (string, string, error) {
	return r.CreateAPIKey(ctx, projectID)
}

// ListAPIKeysForProject returns API keys newest-first for admin display.
func (r *PostgresRepository) ListAPIKeysForProject(ctx context.Context, projectID string) ([]APIKeyMeta, error) {
	return r.listAPIKeys(ctx, projectID, true)
}

// DeleteAPIKeyByID revokes an API key scoped to the provided project.
func (r *PostgresRepository) DeleteAPIKeyByID(ctx context.Context, projectID, keyID string) error {
	return r.DeleteAPIKey(ctx, projectID, keyID)
}
