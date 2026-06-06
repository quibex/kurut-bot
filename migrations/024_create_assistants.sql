-- +goose Up
CREATE TABLE assistants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id INTEGER NOT NULL UNIQUE,
    label TEXT,
    added_by INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_assistants_telegram_id ON assistants(telegram_id);

-- +goose Down
DROP TABLE assistants;
