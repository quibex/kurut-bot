-- +goose Up
ALTER TABLE pending_orders ADD COLUMN is_migration BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE pending_orders DROP COLUMN is_migration;
