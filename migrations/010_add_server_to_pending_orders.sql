-- +goose Up
ALTER TABLE pending_orders
    ADD COLUMN server_id INTEGER;
ALTER TABLE pending_orders
    ADD COLUMN server_name TEXT;

-- +goose Down
ALTER TABLE pending_orders DROP COLUMN server_id;
ALTER TABLE pending_orders DROP COLUMN server_name;
