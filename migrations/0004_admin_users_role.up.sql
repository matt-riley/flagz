-- +goose Up
ALTER TABLE admin_users ADD COLUMN role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'viewer'));
