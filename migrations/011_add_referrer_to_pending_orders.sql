-- +goose Up
ALTER TABLE pending_orders
    ADD COLUMN referrer_whatsapp TEXT;
ALTER TABLE pending_orders
    ADD COLUMN referrer_subscription_id INTEGER;

-- +goose Down
ALTER TABLE pending_orders DROP COLUMN referrer_whatsapp;
ALTER TABLE pending_orders DROP COLUMN referrer_subscription_id;

