package repository

import (
	"context"
	"fmt"
)

// GetAdminUserByID retrieves an admin user by ID.
func (r *PostgresRepository) GetAdminUserByID(ctx context.Context, id string) (AdminUser, error) {
	var u AdminUser
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at, updated_at
		FROM admin_users
		WHERE id = $1
	`, id).Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.Role,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return AdminUser{}, fmt.Errorf("get admin user by id: %w", err)
	}
	return u, nil
}
