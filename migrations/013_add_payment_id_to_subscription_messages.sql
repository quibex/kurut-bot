-- +goose Up
ALTER TABLE subscription_messages ADD COLUMN payment_id INTEGER REFERENCES payments(id);

-- +goose Down
ALTER TABLE subscription_messages DROP COLUMN payment_id;
