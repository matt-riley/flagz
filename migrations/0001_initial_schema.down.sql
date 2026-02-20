-- +goose Down
DROP INDEX IF EXISTS flag_events_flag_key_event_id_idx;
DROP TABLE IF EXISTS flag_events;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS flags;
