// Package repository provides PostgreSQL-backed persistence for feature flags,
// API keys, and flag events. It also handles LISTEN/NOTIFY-based cache
// invalidation so the service layer stays fresh without polling the database
// into submission.
package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultNotifyChannel = "flag_events"
	maxEventBatchSize    = 1000
)

// Flag is the repository-level representation of a feature flag row.
// Note that Enabled has the opposite polarity to [core.Flag].Disabled;
// the service layer handles the conversion.
type Flag struct {
	Key         string          `json:"key"`
	ProjectID   string          `json:"-"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Variants    json.RawMessage `json:"variants"`
	Rules       json.RawMessage `json:"rules"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// Project represents a tenant or namespace for flags.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AdminUser represents an administrator account.
type AdminUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AdminSession represents an authenticated admin session.
type AdminSession struct {
	IDHash      string    `json:"-"`
	AdminUserID string    `json:"admin_user_id"`
	CSRFToken   string    `json:"csrf_token"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// APIKey represents a stored API key record used for bearer-token authentication.
type APIKey struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"key_hash"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// APIKeyMeta contains non-sensitive metadata for an API key, suitable for
// listing keys without exposing secrets.
type APIKeyMeta struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
}

// FlagEvent represents a change event for a flag, stored in the flag_events
// table and used to drive SSE and gRPC streaming.
type FlagEvent struct {
	EventID   int64           `json:"event_id"`
	ProjectID string          `json:"project_id"`
	FlagKey   string          `json:"flag_key"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// AuditLogEntry records a mutation performed on a flag for audit purposes.
//
// Note: The audit_log table is created by migration 0004_audit_log in the
// phase-4.3/audit-logging branch. This code is included here for interface
// completeness and will function once that migration is applied.
type AuditLogEntry struct {
	ID          int64           `json:"id"`
	ProjectID   string          `json:"project_id"`
	APIKeyID    string          `json:"api_key_id,omitempty"`
	AdminUserID string          `json:"admin_user_id,omitempty"`
	Action      string          `json:"action"`
	FlagKey     string          `json:"flag_key"`
	Details     json.RawMessage `json:"details,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// PostgresRepository implements flag, API key, and event persistence backed by
// a pgxpool connection pool. It also supports LISTEN/NOTIFY for real-time
// cache invalidation.
type PostgresRepository struct {
	pool          *pgxpool.Pool
	notifyChannel string
}

// NewPostgresRepository creates a [PostgresRepository] using the default
// "flag_events" notification channel.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return NewPostgresRepositoryWithChannel(pool, defaultNotifyChannel)
}

// NewPostgresRepositoryWithChannel creates a [PostgresRepository] using the
// specified LISTEN/NOTIFY channel name for flag event notifications.
func NewPostgresRepositoryWithChannel(pool *pgxpool.Pool, notifyChannel string) *PostgresRepository {
	return &PostgresRepository{
		pool:          pool,
		notifyChannel: normalizeNotifyChannel(notifyChannel),
	}
}

// CreateFlag inserts a new flag row and returns the created record with
// server-generated timestamps.
func (r *PostgresRepository) CreateFlag(ctx context.Context, flag Flag) (Flag, error) {
	var created Flag
	err := r.pool.QueryRow(ctx, `
		INSERT INTO flags (project_id, key, description, enabled, variants, rules)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING project_id, key, description, enabled, variants, rules, created_at, updated_at
	`,
		flag.ProjectID,
		flag.Key,
		flag.Description,
		flag.Enabled,
		ensureJSON(flag.Variants, "{}"),
		ensureJSON(flag.Rules, "[]"),
	).Scan(
		&created.ProjectID,
		&created.Key,
		&created.Description,
		&created.Enabled,
		&created.Variants,
		&created.Rules,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		return Flag{}, fmt.Errorf("create flag: %w", err)
	}

	return created, nil
}

// UpdateFlag updates an existing flag row identified by project_id and key and returns the
// updated record. Returns pgx.ErrNoRows (wrapped) if the flag does not exist.
func (r *PostgresRepository) UpdateFlag(ctx context.Context, flag Flag) (Flag, error) {
	var updated Flag
	err := r.pool.QueryRow(ctx, `
		UPDATE flags
		SET description = $3,
		    enabled = $4,
		    variants = $5,
		    rules = $6,
		    updated_at = NOW()
		WHERE project_id = $1 AND key = $2
		RETURNING project_id, key, description, enabled, variants, rules, created_at, updated_at
	`,
		flag.ProjectID,
		flag.Key,
		flag.Description,
		flag.Enabled,
		ensureJSON(flag.Variants, "{}"),
		ensureJSON(flag.Rules, "[]"),
	).Scan(
		&updated.ProjectID,
		&updated.Key,
		&updated.Description,
		&updated.Enabled,
		&updated.Variants,
		&updated.Rules,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		return Flag{}, fmt.Errorf("update flag: %w", err)
	}

	return updated, nil
}

// GetFlag retrieves a single flag by its project_id and key. Returns pgx.ErrNoRows (wrapped)
// if not found.
func (r *PostgresRepository) GetFlag(ctx context.Context, projectID, key string) (Flag, error) {
	var flag Flag
	err := r.pool.QueryRow(ctx, `
		SELECT project_id, key, description, enabled, variants, rules, created_at, updated_at
		FROM flags
		WHERE project_id = $1 AND key = $2
	`, projectID, key).Scan(
		&flag.ProjectID,
		&flag.Key,
		&flag.Description,
		&flag.Enabled,
		&flag.Variants,
		&flag.Rules,
		&flag.CreatedAt,
		&flag.UpdatedAt,
	)
	if err != nil {
		return Flag{}, fmt.Errorf("get flag: %w", err)
	}

	return flag, nil
}

// ListFlags returns all flags across all projects ordered by project_id and key.
func (r *PostgresRepository) ListFlags(ctx context.Context) ([]Flag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT project_id, key, description, enabled, variants, rules, created_at, updated_at
		FROM flags
		ORDER BY project_id, key
	`)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
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

// DeleteFlag removes a flag by project_id and key. Returns pgx.ErrNoRows (wrapped) if the
// flag does not exist.
func (r *PostgresRepository) DeleteFlag(ctx context.Context, projectID, key string) error {
	commandTag, err := r.pool.Exec(ctx, `DELETE FROM flags WHERE project_id = $1 AND key = $2`, projectID, key)
	if err != nil {
		return fmt.Errorf("delete flag: %w", err)
	}
	if err := deleteFlagNoRows(commandTag); err != nil {
		return err
	}

	return nil
}

// ValidateAPIKey returns the stored hash and project ID for a non-revoked key ID.
// Callers should do constant-time comparison outside this package.
func (r *PostgresRepository) ValidateAPIKey(ctx context.Context, id string) (string, string, error) {
	var keyHash string
	var projectID string
	if err := r.pool.QueryRow(ctx, `
		SELECT key_hash, project_id
		FROM api_keys
		WHERE id = $1
		  AND revoked_at IS NULL
	`, id).Scan(&keyHash, &projectID); err != nil {
		return "", "", fmt.Errorf("validate api key: %w", err)
	}

	return keyHash, projectID, nil
}

// CreateAPIKey generates a new API key for the given project, storing a bcrypt
// hash of the secret. The raw secret is returned exactly once; it cannot be
// retrieved later.
func (r *PostgresRepository) CreateAPIKey(ctx context.Context, projectID string) (string, string, error) {
	keyID, err := generateRandomHex(16)
	if err != nil {
		return "", "", fmt.Errorf("generate key id: %w", err)
	}

	secret, err := generateRandomHex(32)
	if err != nil {
		return "", "", fmt.Errorf("generate secret: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("hash api key: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO api_keys (id, project_id, name, key_hash)
		VALUES ($1, $2, $3, $4)
	`, keyID, projectID, "api-key-"+keyID[:8], string(hash))
	if err != nil {
		return "", "", fmt.Errorf("create api key: %w", err)
	}

	return keyID, secret, nil
}

// ListAPIKeys returns metadata for all non-revoked API keys belonging to the
// given project. Secrets are never included.
func (r *PostgresRepository) ListAPIKeys(ctx context.Context, projectID string) ([]APIKeyMeta, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, project_id, created_at
		FROM api_keys
		WHERE project_id = $1 AND revoked_at IS NULL
		ORDER BY created_at
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

// DeleteAPIKey soft-deletes an API key by setting its revoked_at timestamp.
// Returns pgx.ErrNoRows (wrapped) if the key does not exist or is already
// revoked.
func (r *PostgresRepository) DeleteAPIKey(ctx context.Context, projectID, keyID string) error {
	commandTag, err := r.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = NOW()
		WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL
	`, keyID, projectID)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("delete api key: %w", pgx.ErrNoRows)
	}
	return nil
}

// ListEventsSince returns up to 1000 flag events with IDs greater than
// eventID, ordered by event ID.
func (r *PostgresRepository) ListEventsSince(ctx context.Context, projectID string, eventID int64) ([]FlagEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT event_id, project_id, flag_key, event_type, payload, created_at
		FROM flag_events
		WHERE event_id > $1 AND project_id = $2
		ORDER BY event_id
		LIMIT $3
	`, eventID, projectID, maxEventBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list events since: %w", err)
	}
	defer rows.Close()

	events := make([]FlagEvent, 0)
	for rows.Next() {
		var event FlagEvent
		if err := rows.Scan(
			&event.EventID,
			&event.ProjectID,
			&event.FlagKey,
			&event.EventType,
			&event.Payload,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events rows: %w", err)
	}

	return events, nil
}

// ListEventsSinceForKey returns up to maxEventBatchSize flag events with IDs greater than
// eventID for the specified project and flag key. Including projectID in the filter ensures
// that events are correctly scoped when different projects reuse the same flag keys.
func (r *PostgresRepository) ListEventsSinceForKey(ctx context.Context, projectID string, eventID int64, key string) ([]FlagEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT event_id, project_id, flag_key, event_type, payload, created_at
		FROM flag_events
		WHERE event_id > $1
		  AND project_id = $2 AND flag_key = $3
		ORDER BY event_id
		LIMIT $4
	`, eventID, projectID, key, maxEventBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list events since for key: %w", err)
	}
	defer rows.Close()

	events := make([]FlagEvent, 0)
	for rows.Next() {
		var event FlagEvent
		if err := rows.Scan(
			&event.EventID,
			&event.ProjectID,
			&event.FlagKey,
			&event.EventType,
			&event.Payload,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events rows: %w", err)
	}

	return events, nil
}

// CreateProject inserts a new project.
func (r *PostgresRepository) CreateProject(ctx context.Context, name, description string) (Project, error) {
	var p Project
	err := r.pool.QueryRow(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at
	`, name, description).Scan(
		&p.ID,
		&p.Name,
		&p.Description,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// ListProjects returns all projects.
func (r *PostgresRepository) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, description, created_at, updated_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// GetProject retrieves a project by ID.
func (r *PostgresRepository) GetProject(ctx context.Context, id string) (Project, error) {
	var p Project
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM projects
		WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

// CreateAdminUser inserts a new admin user.
func (r *PostgresRepository) CreateAdminUser(ctx context.Context, username, passwordHash string) (AdminUser, error) {
	var u AdminUser
	err := r.pool.QueryRow(ctx, `
		INSERT INTO admin_users (username, password_hash)
		VALUES ($1, $2)
		RETURNING id, username, created_at, updated_at
	`, username, passwordHash).Scan(
		&u.ID,
		&u.Username,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return AdminUser{}, fmt.Errorf("create admin user: %w", err)
	}
	return u, nil
}

// GetAdminUserByUsername retrieves an admin user by username.
func (r *PostgresRepository) GetAdminUserByUsername(ctx context.Context, username string) (AdminUser, error) {
	var u AdminUser
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, created_at, updated_at
		FROM admin_users
		WHERE username = $1
	`, username).Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		return AdminUser{}, fmt.Errorf("get admin user: %w", err)
	}
	return u, nil
}

// HasAdminUsers returns true if any admin user exists.
func (r *PostgresRepository) HasAdminUsers(ctx context.Context) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check admin users: %w", err)
	}
	return exists, nil
}

// CreateAdminSession creates a new session.
func (r *PostgresRepository) CreateAdminSession(ctx context.Context, session AdminSession) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO admin_sessions (id_hash, admin_user_id, csrf_token, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, session.IDHash, session.AdminUserID, session.CSRFToken, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create admin session: %w", err)
	}
	return nil
}

// GetAdminSession retrieves a session by ID hash.
func (r *PostgresRepository) GetAdminSession(ctx context.Context, idHash string) (AdminSession, error) {
	var s AdminSession
	err := r.pool.QueryRow(ctx, `
		SELECT id_hash, admin_user_id, csrf_token, created_at, expires_at
		FROM admin_sessions
		WHERE id_hash = $1 AND expires_at > NOW()
	`, idHash).Scan(
		&s.IDHash,
		&s.AdminUserID,
		&s.CSRFToken,
		&s.CreatedAt,
		&s.ExpiresAt,
	)
	if err != nil {
		return AdminSession{}, fmt.Errorf("get admin session: %w", err)
	}
	return s, nil
}

// DeleteAdminSession removes a session.
func (r *PostgresRepository) DeleteAdminSession(ctx context.Context, idHash string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE id_hash = $1`, idHash)
	if err != nil {
		return fmt.Errorf("delete admin session: %w", err)
	}
	return nil
}

// DeleteExpiredAdminSessions removes all sessions that have passed their expiry time.
func (r *PostgresRepository) DeleteExpiredAdminSessions(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("delete expired admin sessions: %w", err)
	}
	return nil
}

// PublishFlagEvent inserts a flag event and sends a PostgreSQL NOTIFY on the
// configured channel within a single transaction.
func (r *PostgresRepository) PublishFlagEvent(ctx context.Context, event FlagEvent) (FlagEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FlagEvent{}, fmt.Errorf("begin publish event tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var created FlagEvent
	if err := tx.QueryRow(ctx, `
		INSERT INTO flag_events (project_id, flag_key, event_type, payload)
		VALUES ($1, $2, $3, $4)
		RETURNING event_id, project_id, flag_key, event_type, payload, created_at
	`,
		event.ProjectID,
		event.FlagKey,
		event.EventType,
		ensureJSON(event.Payload, "{}"),
	).Scan(
		&created.EventID,
		&created.ProjectID,
		&created.FlagKey,
		&created.EventType,
		&created.Payload,
		&created.CreatedAt,
	); err != nil {
		return FlagEvent{}, fmt.Errorf("insert flag event: %w", err)
	}

	notifyPayload, err := marshalNotifyPayload(created)
	if err != nil {
		return FlagEvent{}, fmt.Errorf("marshal notify payload: %w", err)
	}

	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, r.notifyChannel, notifyPayload); err != nil {
		return FlagEvent{}, fmt.Errorf("notify flag event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return FlagEvent{}, fmt.Errorf("commit publish event tx: %w", err)
	}

	return created, nil
}

// SubscribeFlagInvalidation returns a channel that receives a signal whenever a
// flag event notification arrives on the PostgreSQL LISTEN channel. The channel
// is closed if the underlying connection is lost.
func (r *PostgresRepository) SubscribeFlagInvalidation(ctx context.Context) (<-chan struct{}, error) {
	invalidations := make(chan struct{}, 1)

	go r.runFlagInvalidationListener(ctx, invalidations)

	return invalidations, nil
}

func (r *PostgresRepository) runFlagInvalidationListener(ctx context.Context, invalidations chan<- struct{}) {
	defer close(invalidations)

	for {
		err := r.listenForFlagInvalidation(ctx, invalidations)
		if err == nil || ctx.Err() != nil {
			return
		}

		retryTimer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			retryTimer.Stop()
			return
		case <-retryTimer.C:
		}
	}
}

func (r *PostgresRepository) listenForFlagInvalidation(ctx context.Context, invalidations chan<- struct{}) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire listen connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, listenStatement(r.notifyChannel)); err != nil {
		return fmt.Errorf("listen on %q: %w", r.notifyChannel, err)
	}

	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait for flag event notification: %w", err)
		}

		select {
		case invalidations <- struct{}{}:
		default:
		}
	}
}

func deleteFlagNoRows(commandTag pgconn.CommandTag) error {
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("delete flag: %w", pgx.ErrNoRows)
	}

	return nil
}

func normalizeNotifyChannel(channel string) string {
	if trimmed := strings.TrimSpace(channel); trimmed != "" {
		return trimmed
	}

	return defaultNotifyChannel
}

func ensureJSON(input json.RawMessage, fallback string) json.RawMessage {
	if len(input) == 0 {
		return json.RawMessage(fallback)
	}

	return input
}

// InsertAuditLog writes a single audit log entry and returns the generated ID.
func (r *PostgresRepository) InsertAuditLog(ctx context.Context, entry AuditLogEntry) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO audit_log (project_id, api_key_id, admin_user_id, action, flag_key, details)
 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ProjectID, entry.APIKeyID, entry.AdminUserID, entry.Action, entry.FlagKey, entry.Details,
	)
	return err
}

// ListAuditLog returns audit log entries for a project, newest first.
func (r *PostgresRepository) ListAuditLog(ctx context.Context, projectID string, limit, offset int) ([]AuditLogEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, project_id, api_key_id, admin_user_id, action, flag_key, details, created_at
 FROM audit_log
 WHERE project_id = $1
 ORDER BY id DESC
 LIMIT $2 OFFSET $3`,
		projectID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("listing audit log: %w", err)
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.APIKeyID, &e.AdminUserID, &e.Action, &e.FlagKey, &e.Details, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning audit log entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit log rows: %w", err)
	}
	return entries, nil
}

func listenStatement(channel string) string {
	return fmt.Sprintf("LISTEN %s", pgx.Identifier{channel}.Sanitize())
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func marshalNotifyPayload(event FlagEvent) (string, error) {
	serialized, err := json.Marshal(struct {
		ProjectID string `json:"project_id"`
		FlagKey   string `json:"flag_key"`
		EventType string `json:"event_type"`
	}{
		ProjectID: event.ProjectID,
		FlagKey:   event.FlagKey,
		EventType: event.EventType,
	})
	if err != nil {
		return "", err
	}

	return string(serialized), nil
}
