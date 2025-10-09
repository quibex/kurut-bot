-- +goose Up
ALTER TABLE subscriptions ADD COLUMN client_name TEXT;

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN client_name;

