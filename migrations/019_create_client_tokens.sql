-- +goose Up
CREATE TABLE client_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT NOT NULL UNIQUE,
    whatsapp TEXT NOT NULL UNIQUE,
    created_by_telegram_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_client_tokens_token ON client_tokens(token);
CREATE INDEX idx_client_tokens_whatsapp ON client_tokens(whatsapp);

-- +goose Down
DROP TABLE client_tokens;
