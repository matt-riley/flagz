-- +goose Up
-- New tables
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE admin_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,          -- Argon2id encoded string
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE admin_sessions (
    id_hash TEXT PRIMARY KEY,              -- SHA-256 hash of the random session token
    admin_user_id UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    csrf_token TEXT NOT NULL               -- Anti-CSRF token
);

-- Insert default project with a specific UUID (not nil) to avoid ambiguity
INSERT INTO projects (id, name, description) VALUES
    ('11111111-1111-1111-1111-111111111111', 'Default', 'Auto-created project for pre-existing flags');

-- Modify flags table for multi-tenancy
ALTER TABLE flags ADD COLUMN project_id UUID REFERENCES projects(id);
UPDATE flags SET project_id = '11111111-1111-1111-1111-111111111111' WHERE project_id IS NULL;
ALTER TABLE flags ALTER COLUMN project_id SET NOT NULL;

-- Drop old PK and add composite PK (project_id, key) to allow same key in different projects
ALTER TABLE flags DROP CONSTRAINT flags_pkey;
ALTER TABLE flags ADD PRIMARY KEY (project_id, key);

-- Modify api_keys table
ALTER TABLE api_keys ADD COLUMN project_id UUID REFERENCES projects(id);
UPDATE api_keys SET project_id = '11111111-1111-1111-1111-111111111111' WHERE project_id IS NULL;
ALTER TABLE api_keys ALTER COLUMN project_id SET NOT NULL;

-- Modify flag_events table
ALTER TABLE flag_events ADD COLUMN project_id UUID REFERENCES projects(id);
UPDATE flag_events SET project_id = '11111111-1111-1111-1111-111111111111' WHERE project_id IS NULL;
ALTER TABLE flag_events ALTER COLUMN project_id SET NOT NULL;

-- Add indexes for new query patterns
CREATE INDEX idx_flag_events_project_event ON flag_events (project_id, event_id);
CREATE INDEX idx_flag_events_project_key_event ON flag_events (project_id, flag_key, event_id);
