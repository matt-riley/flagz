-- +goose Down
-- Revert api_keys changes
ALTER TABLE api_keys DROP COLUMN project_id;

-- Revert flags changes
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
