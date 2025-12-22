-- +goose Up
CREATE TABLE subscription_messages
(
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id    INTEGER NOT NULL,
    chat_id            INTEGER NOT NULL,
    message_id         INTEGER NOT NULL,
    type               TEXT    NOT NULL CHECK (type IN ('expiring', 'overdue')),
    is_active          BOOLEAN   DEFAULT TRUE,
    selected_tariff_id INTEGER,
    created_at         TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (subscription_id) REFERENCES subscriptions (id) ON DELETE CASCADE
);

CREATE INDEX idx_sub_messages_subscription ON subscription_messages (subscription_id);
CREATE INDEX idx_sub_messages_active ON subscription_messages (is_active);
CREATE INDEX idx_sub_messages_chat ON subscription_messages (chat_id, message_id);

-- +goose Down
DROP TABLE subscription_messages;
