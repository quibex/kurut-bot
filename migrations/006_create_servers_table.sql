-- +goose Up
CREATE TABLE servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    ui_url TEXT NOT NULL,
    ui_password TEXT NOT NULL,
    current_users INTEGER NOT NULL DEFAULT 0,
    max_users INTEGER NOT NULL DEFAULT 150,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_servers_archived ON servers(archived);

-- Add server_id to subscriptions table
ALTER TABLE subscriptions ADD COLUMN server_id INTEGER REFERENCES servers(id);
CREATE INDEX idx_subscriptions_server_id ON subscriptions(server_id);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_server_id;
ALTER TABLE subscriptions DROP COLUMN server_id;
DROP TABLE servers;
