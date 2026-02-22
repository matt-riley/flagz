-- +goose Down
-- WARNING: If multiple projects use the same flag key, this migration will fail.
-- You must delete or merge duplicate keys before rolling back.

-- Back up non-default-project flags before deleting them
CREATE TABLE IF NOT EXISTS _flags_backup_0003 AS
  SELECT * FROM flags WHERE project_id != '11111111-1111-1111-1111-111111111111';

-- Revert api_keys changes
DROP INDEX IF EXISTS idx_api_keys_project_id;
ALTER TABLE api_keys DROP COLUMN project_id;

-- Revert flags changes (will fail if duplicate keys exist across projects)
DELETE FROM flags WHERE project_id != '11111111-1111-1111-1111-111111111111';
ALTER TABLE flags DROP CONSTRAINT flags_pkey;
ALTER TABLE flags ADD PRIMARY KEY (key);
ALTER TABLE flags DROP COLUMN project_id;

-- Revert flag_events changes
DROP INDEX IF EXISTS idx_flag_events_project_key_event;
DROP INDEX IF EXISTS idx_flag_events_project_event;
ALTER TABLE flag_events DROP COLUMN project_id;

-- Drop new tables
DROP TABLE admin_sessions;
DROP TABLE admin_users;
DROP TABLE projects;
