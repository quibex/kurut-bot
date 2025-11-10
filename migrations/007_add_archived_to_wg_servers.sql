-- +goose Up
ALTER TABLE wg_servers ADD COLUMN archived BOOLEAN DEFAULT 0;

CREATE INDEX idx_wg_servers_archived ON wg_servers(archived);

-- +goose Down
DROP INDEX idx_wg_servers_archived;
ALTER TABLE wg_servers DROP COLUMN archived;
