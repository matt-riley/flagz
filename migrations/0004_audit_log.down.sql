-- +goose Down
DROP INDEX IF EXISTS idx_audit_log_project_created;
DROP TABLE IF EXISTS audit_log;
