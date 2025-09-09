-- +goose Up
CREATE TABLE subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    tariff_id INTEGER NOT NULL,
    marzban_user_id TEXT NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMP,
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (tariff_id) REFERENCES tariffs(id)
);

CREATE INDEX idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX idx_subscriptions_tariff_id ON subscriptions(tariff_id);
CREATE INDEX idx_subscriptions_marzban_user_id ON subscriptions(marzban_user_id);
CREATE INDEX idx_subscriptions_status ON subscriptions(status);
CREATE INDEX idx_subscriptions_expires_at ON subscriptions(expires_at);

-- +goose Down
DROP TABLE subscriptions;
