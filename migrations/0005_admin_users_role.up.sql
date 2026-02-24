-- +goose Up
ALTER TABLE admin_users ADD COLUMN role TEXT;

UPDATE admin_users
SET role = 'admin'
WHERE role IS NULL;

ALTER TABLE admin_users ALTER COLUMN role SET DEFAULT 'viewer';
ALTER TABLE admin_users ALTER COLUMN role SET NOT NULL;
ALTER TABLE admin_users ADD CONSTRAINT admin_users_role_check CHECK (role IN ('admin', 'viewer'));
