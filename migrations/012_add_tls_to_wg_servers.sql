-- +goose Up
ALTER TABLE wg_servers ADD COLUMN tls_enabled BOOLEAN DEFAULT 0;
ALTER TABLE wg_servers ADD COLUMN tls_cert_path TEXT;
ALTER TABLE wg_servers ADD COLUMN tls_server_name TEXT;

-- +goose Down
ALTER TABLE wg_servers DROP COLUMN tls_server_name;
ALTER TABLE wg_servers DROP COLUMN tls_cert_path;
ALTER TABLE wg_servers DROP COLUMN tls_enabled;

