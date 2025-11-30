-- +goose Up
ALTER TABLE wg_servers ADD COLUMN health_endpoint TEXT DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN directly
-- This would require recreating the table
