-- +goose Up
CREATE TABLE audit_log (
  id BIGSERIAL PRIMARY KEY,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  api_key_id TEXT NOT NULL DEFAULT '',
  admin_user_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  flag_key TEXT NOT NULL DEFAULT '',
  details JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_log_project_created ON audit_log (project_id, id DESC);
