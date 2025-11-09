-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id INTEGER UNIQUE NOT NULL,
    used_trial BOOLEAN DEFAULT FALSE,
    language TEXT DEFAULT '' NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_telegram_id ON users(telegram_id);
CREATE INDEX idx_users_used_trial ON users(used_trial);
CREATE INDEX idx_users_language ON users(language);

-- +goose Down
DROP TABLE users;


