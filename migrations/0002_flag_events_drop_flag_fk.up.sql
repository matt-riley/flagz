-- +goose Up
ALTER TABLE flag_events
    DROP CONSTRAINT IF EXISTS flag_events_flag_key_fkey;
