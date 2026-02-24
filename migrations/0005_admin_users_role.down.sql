-- +goose Down
ALTER TABLE admin_users DROP COLUMN role;
