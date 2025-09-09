-- +goose Up
CREATE TABLE payment_subscriptions (
    payment_id INTEGER NOT NULL,
    subscription_id INTEGER NOT NULL,
    PRIMARY KEY (payment_id, subscription_id),
    FOREIGN KEY (payment_id) REFERENCES payments(id),
    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id)
);

CREATE INDEX idx_payment_subscriptions_payment_id ON payment_subscriptions(payment_id);
CREATE INDEX idx_payment_subscriptions_subscription_id ON payment_subscriptions(subscription_id);

-- +goose Down
DROP TABLE payment_subscriptions;
