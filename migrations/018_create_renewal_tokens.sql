-- +goose Up
-- +goose StatementBegin
CREATE TABLE renewal_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT NOT NULL UNIQUE,
    subscription_id INTEGER NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id)
);

CREATE INDEX idx_renewal_tokens_token ON renewal_tokens(token);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_renewal_tokens_token;
DROP TABLE IF EXISTS renewal_tokens;
-- +goose StatementEnd
