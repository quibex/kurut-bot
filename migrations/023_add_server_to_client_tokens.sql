-- +goose Up
ALTER TABLE client_tokens ADD COLUMN server_id INTEGER;
ALTER TABLE client_tokens ADD COLUMN server_name TEXT;

-- +goose Down
ALTER TABLE client_tokens DROP COLUMN server_id;
ALTER TABLE client_tokens DROP COLUMN server_name;
