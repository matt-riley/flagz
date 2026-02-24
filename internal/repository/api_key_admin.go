package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/matt-riley/flagz/internal/middleware"
)

// CreateAPIKeyForProject generates a new API key for the given project and
// returns the key ID and raw secret. The secret is bcrypt-hashed before
// storage so it cannot be recovered later.
func (r *PostgresRepository) CreateAPIKeyForProject(ctx context.Context, projectID string) (string, string, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return "", "", fmt.Errorf("generate key id: %w", err)
	}
	keyID := hex.EncodeToString(idBytes)

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("generate key secret: %w", err)
	}
	rawSecret := hex.EncodeToString(secretBytes)

	keyHash, err := middleware.HashAPIKey(rawSecret)
	if err != nil {
		return "", "", fmt.Errorf("hash key: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
INSERT INTO api_keys (id, project_id, name, key_hash)
VALUES ($1, $2, $3, $4)
`, keyID, projectID, "admin-generated", keyHash)
	if err != nil {
		return "", "", fmt.Errorf("create api key: %w", err)
	}

	return keyID, rawSecret, nil
}

// ListAPIKeysForProject returns metadata for all non-revoked API keys belonging
// to the specified project.
func (r *PostgresRepository) ListAPIKeysForProject(ctx context.Context, projectID string) ([]APIKeyMeta, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, project_id, created_at
FROM api_keys
WHERE project_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC
`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	keys := make([]APIKeyMeta, 0)
	for rows.Next() {
		var k APIKeyMeta
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list api keys rows: %w", err)
	}
	return keys, nil
}

// DeleteAPIKeyByID revokes an API key by setting its revoked_at timestamp,
// scoped to the given project to prevent cross-project deletion.
func (r *PostgresRepository) DeleteAPIKeyByID(ctx context.Context, projectID, keyID string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE api_keys SET revoked_at = NOW()
WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL
`, keyID, projectID)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found or already revoked")
	}
	return nil
}
