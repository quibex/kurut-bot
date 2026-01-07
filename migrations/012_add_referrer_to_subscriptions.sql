-- +goose Up
ALTER TABLE subscriptions
    ADD COLUMN referrer_whatsapp TEXT;

CREATE INDEX idx_subscriptions_referrer_whatsapp ON subscriptions(referrer_whatsapp);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_referrer_whatsapp;
ALTER TABLE subscriptions DROP COLUMN referrer_whatsapp;

