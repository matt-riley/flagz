//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/docker/go-connections/nat"
	"golang.org/x/crypto/bcrypt"

	"github.com/matt-riley/flagz/internal/repository"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:18-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "flagz_test",
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
		},
		WaitingFor: wait.ForSQL("5432/tcp", "pgx", func(host string, port nat.Port) string {
			return fmt.Sprintf("postgresql://test:test@%s:%s/flagz_test?sslmode=disable", host, port.Port())
		}).WithStartupTimeout(30 * time.Second),
	}

	pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		log.Printf("start postgres container: %v", err)
		return 1
	}
	defer func() { _ = pgContainer.Terminate(ctx) }()

	host, err := pgContainer.Host(ctx)
	if err != nil {
		log.Printf("get container host: %v", err)
		return 1
	}

	mappedPort, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		log.Printf("get mapped port: %v", err)
		return 1
	}

	connStr := fmt.Sprintf(
		"postgresql://test:test@%s:%s/flagz_test?sslmode=disable",
		host, mappedPort.Port(),
	)

	// Run goose migrations.
	migrationsDir, err := findMigrationsDir()
	if err != nil {
		log.Printf("find migrations: %v", err)
		return 1
	}
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Printf("open db for migrations: %v", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close db after migrations: %v", err)
		}
	}()
	if err := goose.SetDialect("postgres"); err != nil {
		log.Printf("set goose dialect: %v", err)
		return 1
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		log.Printf("run migrations: %v", err)
		return 1
	}

	// Create pgxpool for repository usage.
	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		log.Printf("create pool: %v", err)
		return 1
	}
	defer testPool.Close()

	return m.Run()
}

// findMigrationsDir walks up from the working directory until it finds a
// migrations/ directory (the repository root contains it).
func findMigrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "migrations")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("migrations directory not found")
		}
		dir = parent
	}
}

func newRepo() *repository.PostgresRepository {
	return repository.NewPostgresRepository(testPool)
}

func randID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}

func createTestProject(t *testing.T, repo *repository.PostgresRepository, suffix string) repository.Project {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("test-%s-%s", suffix, randID())
	p, err := repo.CreateProject(ctx, name, "integration test project")
	if err != nil {
		t.Fatalf("create test project: %v", err)
	}
	return p
}

// insertAPIKey inserts an API key directly and returns (keyID, rawSecret).
func insertAPIKey(t *testing.T, projectID string) (string, string) {
	t.Helper()
	keyID := fmt.Sprintf("key-%s", randID())
	rawSecret := fmt.Sprintf("secret-%s", randID())
	// Use bcrypt (current production format) rather than SHA-256 (legacy).
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(rawSecret), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash API key: %v", err)
	}
	keyHash := string(hashBytes)

	_, err = testPool.Exec(context.Background(), `
		INSERT INTO api_keys (id, project_id, name, key_hash)
		VALUES ($1, $2, $3, $4)
	`, keyID, projectID, "test-key", keyHash)
	if err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return keyID, rawSecret
}

// revokeAPIKey sets revoked_at on an API key.
func revokeAPIKey(t *testing.T, keyID string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`UPDATE api_keys SET revoked_at = NOW() WHERE id = $1`, keyID)
	if err != nil {
		t.Fatalf("revoke api key: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Flag CRUD
// ---------------------------------------------------------------------------

func TestFlagCRUD(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	t.Run("create and get", func(t *testing.T) {
		project := createTestProject(t, repo, "create-get")

		flag := repository.Flag{
			Key:         "feature-x",
			ProjectID:   project.ID,
			Description: "test flag",
			Enabled:     true,
		}
		created, err := repo.CreateFlag(ctx, flag)
		if err != nil {
			t.Fatalf("CreateFlag: %v", err)
		}
		if created.Key != flag.Key {
			t.Errorf("Key = %q, want %q", created.Key, flag.Key)
		}
		if created.ProjectID != project.ID {
			t.Errorf("ProjectID = %q, want %q", created.ProjectID, project.ID)
		}
		if created.Description != flag.Description {
			t.Errorf("Description = %q, want %q", created.Description, flag.Description)
		}
		if !created.Enabled {
			t.Error("Enabled = false, want true")
		}
		if created.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}

		got, err := repo.GetFlag(ctx, project.ID, flag.Key)
		if err != nil {
			t.Fatalf("GetFlag: %v", err)
		}
		if got.Key != created.Key {
			t.Errorf("got Key = %q, want %q", got.Key, created.Key)
		}
		if got.Description != created.Description {
			t.Errorf("got Description = %q, want %q", got.Description, created.Description)
		}
	})

	t.Run("create with variants and rules", func(t *testing.T) {
		project := createTestProject(t, repo, "variants")

		flag := repository.Flag{
			Key:       "ab-test",
			ProjectID: project.ID,
			Enabled:   true,
			Variants:  json.RawMessage(`{"control":"off","experiment":"on"}`),
			Rules:     json.RawMessage(`[{"attribute":"country","operator":"equals","value":"US"}]`),
		}
		created, err := repo.CreateFlag(ctx, flag)
		if err != nil {
			t.Fatalf("CreateFlag: %v", err)
		}

		got, err := repo.GetFlag(ctx, project.ID, created.Key)
		if err != nil {
			t.Fatalf("GetFlag: %v", err)
		}
		if string(got.Variants) == "{}" {
			t.Error("Variants were not persisted")
		}

		var variants map[string]string
		if err := json.Unmarshal(got.Variants, &variants); err != nil {
			t.Fatalf("unmarshal Variants: %v (raw: %s)", err, string(got.Variants))
		}
		if len(variants) != 2 || variants["control"] != "off" || variants["experiment"] != "on" {
			t.Errorf("Variants = %s, want {control:off, experiment:on}", string(got.Variants))
		}

		if string(got.Rules) == "[]" {
			t.Error("Rules were not persisted")
		}

		type rule struct {
			Attribute string `json:"attribute"`
			Operator  string `json:"operator"`
			Value     string `json:"value"`
		}
		var rules []rule
		if err := json.Unmarshal(got.Rules, &rules); err != nil {
			t.Fatalf("unmarshal Rules: %v (raw: %s)", err, string(got.Rules))
		}
		if len(rules) != 1 || rules[0].Attribute != "country" || rules[0].Operator != "equals" || rules[0].Value != "US" {
			t.Errorf("Rules = %s, want [{attribute:country, operator:equals, value:US}]", string(got.Rules))
		}
	})

	t.Run("update", func(t *testing.T) {
		project := createTestProject(t, repo, "update")

		flag := repository.Flag{
			Key:         "feature-y",
			ProjectID:   project.ID,
			Description: "original",
			Enabled:     false,
		}
		_, err := repo.CreateFlag(ctx, flag)
		if err != nil {
			t.Fatalf("CreateFlag: %v", err)
		}

		flag.Description = "updated"
		flag.Enabled = true
		updated, err := repo.UpdateFlag(ctx, flag)
		if err != nil {
			t.Fatalf("UpdateFlag: %v", err)
		}
		if updated.Description != "updated" {
			t.Errorf("Description = %q, want %q", updated.Description, "updated")
		}
		if !updated.Enabled {
			t.Error("Enabled = false, want true")
		}
	})

	t.Run("update nonexistent returns error", func(t *testing.T) {
		project := createTestProject(t, repo, "update-missing")

		_, err := repo.UpdateFlag(ctx, repository.Flag{
			Key:       "nonexistent",
			ProjectID: project.ID,
		})
		if err == nil {
			t.Fatal("expected error for nonexistent flag, got nil")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("error = %v, want wrapping pgx.ErrNoRows", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		project := createTestProject(t, repo, "delete")

		_, err := repo.CreateFlag(ctx, repository.Flag{
			Key:       "to-delete",
			ProjectID: project.ID,
		})
		if err != nil {
			t.Fatalf("CreateFlag: %v", err)
		}

		if err := repo.DeleteFlag(ctx, project.ID, "to-delete"); err != nil {
			t.Fatalf("DeleteFlag: %v", err)
		}

		_, err = repo.GetFlag(ctx, project.ID, "to-delete")
		if err == nil {
			t.Fatal("expected error after delete, got nil")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("error = %v, want wrapping pgx.ErrNoRows", err)
		}
	})

	t.Run("delete nonexistent returns error", func(t *testing.T) {
		project := createTestProject(t, repo, "delete-missing")

		err := repo.DeleteFlag(ctx, project.ID, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent flag, got nil")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("error = %v, want wrapping pgx.ErrNoRows", err)
		}
	})

	t.Run("list flags by project", func(t *testing.T) {
		project := createTestProject(t, repo, "list")

		for _, key := range []string{"alpha", "beta", "gamma"} {
			_, err := repo.CreateFlag(ctx, repository.Flag{
				Key:       key,
				ProjectID: project.ID,
				Enabled:   true,
			})
			if err != nil {
				t.Fatalf("CreateFlag %q: %v", key, err)
			}
		}

		flags, err := repo.ListFlagsByProject(ctx, project.ID)
		if err != nil {
			t.Fatalf("ListFlagsByProject: %v", err)
		}
		if len(flags) != 3 {
			t.Fatalf("got %d flags, want 3", len(flags))
		}
		if flags[0].Key != "alpha" || flags[1].Key != "beta" || flags[2].Key != "gamma" {
			t.Errorf("unexpected order: %q, %q, %q", flags[0].Key, flags[1].Key, flags[2].Key)
		}
	})
}

// ---------------------------------------------------------------------------
// Flag events
// ---------------------------------------------------------------------------

func TestFlagEvents(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	t.Run("publish and list events", func(t *testing.T) {
		project := createTestProject(t, repo, "events")

		published, err := repo.PublishFlagEvent(ctx, repository.FlagEvent{
			ProjectID: project.ID,
			FlagKey:   "event-flag",
			EventType: "updated",
			Payload:   json.RawMessage(`{"enabled": true}`),
		})
		if err != nil {
			t.Fatalf("PublishFlagEvent: %v", err)
		}
		if published.EventID == 0 {
			t.Error("EventID = 0, want nonzero")
		}
		if published.FlagKey != "event-flag" {
			t.Errorf("FlagKey = %q, want %q", published.FlagKey, "event-flag")
		}

		events, err := repo.ListEventsSince(ctx, project.ID, 0)
		if err != nil {
			t.Fatalf("ListEventsSince: %v", err)
		}

		found := false
		for _, e := range events {
			if e.EventID == published.EventID {
				found = true
				if e.EventType != "updated" {
					t.Errorf("EventType = %q, want %q", e.EventType, "updated")
				}
			}
		}
		if !found {
			t.Error("published event not found in ListEventsSince results")
		}
	})

	t.Run("list events since filters by event ID", func(t *testing.T) {
		project := createTestProject(t, repo, "events-filter")

		first, err := repo.PublishFlagEvent(ctx, repository.FlagEvent{
			ProjectID: project.ID,
			FlagKey:   "flag-a",
			EventType: "updated",
			Payload:   json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("PublishFlagEvent first: %v", err)
		}

		second, err := repo.PublishFlagEvent(ctx, repository.FlagEvent{
			ProjectID: project.ID,
			FlagKey:   "flag-b",
			EventType: "updated",
			Payload:   json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("PublishFlagEvent second: %v", err)
		}

		events, err := repo.ListEventsSince(ctx, project.ID, first.EventID)
		if err != nil {
			t.Fatalf("ListEventsSince: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
		if events[0].EventID != second.EventID {
			t.Errorf("EventID = %d, want %d", events[0].EventID, second.EventID)
		}
	})

	t.Run("list events since for key", func(t *testing.T) {
		project := createTestProject(t, repo, "events-key")

		_, err := repo.PublishFlagEvent(ctx, repository.FlagEvent{
			ProjectID: project.ID,
			FlagKey:   "key-a",
			EventType: "updated",
			Payload:   json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("PublishFlagEvent key-a: %v", err)
		}

		keyBEvent, err := repo.PublishFlagEvent(ctx, repository.FlagEvent{
			ProjectID: project.ID,
			FlagKey:   "key-b",
			EventType: "updated",
			Payload:   json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("PublishFlagEvent key-b: %v", err)
		}

		events, err := repo.ListEventsSinceForKey(ctx, project.ID, 0, "key-b")
		if err != nil {
			t.Fatalf("ListEventsSinceForKey: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
		if events[0].EventID != keyBEvent.EventID {
			t.Errorf("EventID = %d, want %d", events[0].EventID, keyBEvent.EventID)
		}
	})
}

// ---------------------------------------------------------------------------
// API key validation
// ---------------------------------------------------------------------------

func TestAPIKeyValidation(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	t.Run("validate correct secret", func(t *testing.T) {
		project := createTestProject(t, repo, "apikey-valid")
		keyID, rawSecret := insertAPIKey(t, project.ID)

		keyHash, projectID, err := repo.ValidateAPIKey(ctx, keyID)
		if err != nil {
			t.Fatalf("ValidateAPIKey: %v", err)
		}
		if projectID != project.ID {
			t.Errorf("projectID = %q, want %q", projectID, project.ID)
		}

		if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(rawSecret)); err != nil {
			t.Errorf("bcrypt hash mismatch: %v", err)
		}
	})

	t.Run("validate nonexistent key returns error", func(t *testing.T) {
		_, _, err := repo.ValidateAPIKey(ctx, "nonexistent-key-id")
		if err == nil {
			t.Fatal("expected error for nonexistent key, got nil")
		}
	})

	t.Run("revoked key fails validation", func(t *testing.T) {
		project := createTestProject(t, repo, "apikey-revoke")
		keyID, _ := insertAPIKey(t, project.ID)

		revokeAPIKey(t, keyID)

		_, _, err := repo.ValidateAPIKey(ctx, keyID)
		if err == nil {
			t.Fatal("expected error for revoked key, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// Project scoping
// ---------------------------------------------------------------------------

func TestProjectScoping(t *testing.T) {
	repo := newRepo()
	ctx := context.Background()

	t.Run("flags in different projects are isolated", func(t *testing.T) {
		projectA := createTestProject(t, repo, "scope-a")
		projectB := createTestProject(t, repo, "scope-b")

		_, err := repo.CreateFlag(ctx, repository.Flag{
			Key:         "shared-name",
			ProjectID:   projectA.ID,
			Description: "project A flag",
			Enabled:     true,
		})
		if err != nil {
			t.Fatalf("CreateFlag A: %v", err)
		}

		_, err = repo.CreateFlag(ctx, repository.Flag{
			Key:         "shared-name",
			ProjectID:   projectB.ID,
			Description: "project B flag",
			Enabled:     false,
		})
		if err != nil {
			t.Fatalf("CreateFlag B: %v", err)
		}

		flagA, err := repo.GetFlag(ctx, projectA.ID, "shared-name")
		if err != nil {
			t.Fatalf("GetFlag A: %v", err)
		}
		if flagA.Description != "project A flag" {
			t.Errorf("flagA Description = %q, want %q", flagA.Description, "project A flag")
		}
		if !flagA.Enabled {
			t.Error("flagA Enabled = false, want true")
		}

		flagB, err := repo.GetFlag(ctx, projectB.ID, "shared-name")
		if err != nil {
			t.Fatalf("GetFlag B: %v", err)
		}
		if flagB.Description != "project B flag" {
			t.Errorf("flagB Description = %q, want %q", flagB.Description, "project B flag")
		}
		if flagB.Enabled {
			t.Error("flagB Enabled = true, want false")
		}

		flagsA, err := repo.ListFlagsByProject(ctx, projectA.ID)
		if err != nil {
			t.Fatalf("ListFlagsByProject A: %v", err)
		}
		if len(flagsA) != 1 {
			t.Fatalf("got %d flags for project A, want 1", len(flagsA))
		}
		if flagsA[0].Description != "project A flag" {
			t.Errorf("flagsA[0] Description = %q, want %q", flagsA[0].Description, "project A flag")
		}

		flagsB, err := repo.ListFlagsByProject(ctx, projectB.ID)
		if err != nil {
			t.Fatalf("ListFlagsByProject B: %v", err)
		}
		if len(flagsB) != 1 {
			t.Fatalf("got %d flags for project B, want 1", len(flagsB))
		}
		if flagsB[0].Description != "project B flag" {
			t.Errorf("flagsB[0] Description = %q, want %q", flagsB[0].Description, "project B flag")
		}
	})

	t.Run("events in different projects are isolated", func(t *testing.T) {
		projectA := createTestProject(t, repo, "event-scope-a")
		projectB := createTestProject(t, repo, "event-scope-b")

		_, err := repo.PublishFlagEvent(ctx, repository.FlagEvent{
			ProjectID: projectA.ID,
			FlagKey:   "scoped-flag",
			EventType: "updated",
			Payload:   json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("PublishFlagEvent A: %v", err)
		}

		eventsB, err := repo.ListEventsSince(ctx, projectB.ID, 0)
		if err != nil {
			t.Fatalf("ListEventsSince B: %v", err)
		}
		if len(eventsB) != 0 {
			t.Errorf("got %d events for project B, want 0", len(eventsB))
		}
	})

	t.Run("deleting flag in one project does not affect other", func(t *testing.T) {
		projectA := createTestProject(t, repo, "del-scope-a")
		projectB := createTestProject(t, repo, "del-scope-b")

		_, err := repo.CreateFlag(ctx, repository.Flag{
			Key:       "same-key",
			ProjectID: projectA.ID,
		})
		if err != nil {
			t.Fatalf("CreateFlag A: %v", err)
		}

		_, err = repo.CreateFlag(ctx, repository.Flag{
			Key:       "same-key",
			ProjectID: projectB.ID,
		})
		if err != nil {
			t.Fatalf("CreateFlag B: %v", err)
		}

		if err := repo.DeleteFlag(ctx, projectA.ID, "same-key"); err != nil {
			t.Fatalf("DeleteFlag A: %v", err)
		}

		_, err = repo.GetFlag(ctx, projectB.ID, "same-key")
		if err != nil {
			t.Fatalf("GetFlag B after deleting A: %v", err)
		}
	})
}
