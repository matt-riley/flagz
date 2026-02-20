package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultNotifyChannel = "flag_events"
	maxEventBatchSize    = 1000
)

type Flag struct {
	Key         string          `json:"key"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Variants    json.RawMessage `json:"variants"`
	Rules       json.RawMessage `json:"rules"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type APIKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"key_hash"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type FlagEvent struct {
	EventID   int64           `json:"event_id"`
	FlagKey   string          `json:"flag_key"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type PostgresRepository struct {
	pool          *pgxpool.Pool
	notifyChannel string
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return NewPostgresRepositoryWithChannel(pool, defaultNotifyChannel)
}

func NewPostgresRepositoryWithChannel(pool *pgxpool.Pool, notifyChannel string) *PostgresRepository {
	return &PostgresRepository{
		pool:          pool,
		notifyChannel: normalizeNotifyChannel(notifyChannel),
	}
}

func (r *PostgresRepository) CreateFlag(ctx context.Context, flag Flag) (Flag, error) {
	var created Flag
	err := r.pool.QueryRow(ctx, `
		INSERT INTO flags (key, description, enabled, variants, rules)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING key, description, enabled, variants, rules, created_at, updated_at
	`,
		flag.Key,
		flag.Description,
		flag.Enabled,
		ensureJSON(flag.Variants, "{}"),
		ensureJSON(flag.Rules, "[]"),
	).Scan(
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

func (r *PostgresRepository) UpdateFlag(ctx context.Context, flag Flag) (Flag, error) {
	var updated Flag
	err := r.pool.QueryRow(ctx, `
		UPDATE flags
		SET description = $2,
		    enabled = $3,
		    variants = $4,
		    rules = $5,
		    updated_at = NOW()
		WHERE key = $1
		RETURNING key, description, enabled, variants, rules, created_at, updated_at
	`,
		flag.Key,
		flag.Description,
		flag.Enabled,
		ensureJSON(flag.Variants, "{}"),
		ensureJSON(flag.Rules, "[]"),
	).Scan(
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

func (r *PostgresRepository) GetFlag(ctx context.Context, key string) (Flag, error) {
	var flag Flag
	err := r.pool.QueryRow(ctx, `
		SELECT key, description, enabled, variants, rules, created_at, updated_at
		FROM flags
		WHERE key = $1
	`, key).Scan(
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

func (r *PostgresRepository) ListFlags(ctx context.Context) ([]Flag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT key, description, enabled, variants, rules, created_at, updated_at
		FROM flags
		ORDER BY key
	`)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close()

	flags := make([]Flag, 0)
	for rows.Next() {
		var flag Flag
		if err := rows.Scan(
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

func (r *PostgresRepository) DeleteFlag(ctx context.Context, key string) error {
	commandTag, err := r.pool.Exec(ctx, `DELETE FROM flags WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("delete flag: %w", err)
	}
	if err := deleteFlagNoRows(commandTag); err != nil {
		return err
	}

	return nil
}

// ValidateAPIKey returns the stored hash for a non-revoked key ID.
// Callers should do constant-time comparison outside this package.
func (r *PostgresRepository) ValidateAPIKey(ctx context.Context, id string) (string, error) {
	var keyHash string
	if err := r.pool.QueryRow(ctx, `
		SELECT key_hash
		FROM api_keys
		WHERE id = $1
		  AND revoked_at IS NULL
	`, id).Scan(&keyHash); err != nil {
		return "", fmt.Errorf("validate api key: %w", err)
	}

	return keyHash, nil
}

func (r *PostgresRepository) ListEventsSince(ctx context.Context, eventID int64) ([]FlagEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT event_id, flag_key, event_type, payload, created_at
		FROM flag_events
		WHERE event_id > $1
		ORDER BY event_id
		LIMIT $2
	`, eventID, maxEventBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list events since: %w", err)
	}
	defer rows.Close()

	events := make([]FlagEvent, 0)
	for rows.Next() {
		var event FlagEvent
		if err := rows.Scan(
			&event.EventID,
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

func (r *PostgresRepository) ListEventsSinceForKey(ctx context.Context, eventID int64, key string) ([]FlagEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT event_id, flag_key, event_type, payload, created_at
		FROM flag_events
		WHERE event_id > $1
		  AND flag_key = $2
		ORDER BY event_id
		LIMIT $3
	`, eventID, key, maxEventBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list events since for key: %w", err)
	}
	defer rows.Close()

	events := make([]FlagEvent, 0)
	for rows.Next() {
		var event FlagEvent
		if err := rows.Scan(
			&event.EventID,
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

func (r *PostgresRepository) PublishFlagEvent(ctx context.Context, event FlagEvent) (FlagEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return FlagEvent{}, fmt.Errorf("begin publish event tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var created FlagEvent
	if err := tx.QueryRow(ctx, `
		INSERT INTO flag_events (flag_key, event_type, payload)
		VALUES ($1, $2, $3)
		RETURNING event_id, flag_key, event_type, payload, created_at
	`,
		event.FlagKey,
		event.EventType,
		ensureJSON(event.Payload, "{}"),
	).Scan(
		&created.EventID,
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

func listenStatement(channel string) string {
	return fmt.Sprintf("LISTEN %s", pgx.Identifier{channel}.Sanitize())
}

func marshalNotifyPayload(event FlagEvent) (string, error) {
	serialized, err := json.Marshal(struct {
		FlagKey   string `json:"flag_key"`
		EventType string `json:"event_type"`
	}{
		FlagKey:   event.FlagKey,
		EventType: event.EventType,
	})
	if err != nil {
		return "", err
	}

	return string(serialized), nil
}
