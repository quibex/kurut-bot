-- +goose Up
CREATE TABLE wg_servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    grpc_address TEXT NOT NULL,
    interface TEXT DEFAULT 'wg0',
    dns_servers TEXT DEFAULT '1.1.1.1',
    max_peers INTEGER DEFAULT 150,
    current_peers INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_wg_servers_enabled ON wg_servers(enabled);
CREATE INDEX idx_wg_servers_current_peers ON wg_servers(current_peers);

-- +goose Down
DROP TABLE wg_servers;

