-- +goose Up
ALTER TABLE subscriptions
    ADD COLUMN referral_type TEXT;
ALTER TABLE pending_orders
    ADD COLUMN referral_type TEXT;

CREATE INDEX idx_subscriptions_referral_type ON subscriptions(referral_type);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_referral_type;
-- SQLite does not support DROP COLUMN in older versions,
-- but goose with newer SQLite should handle it
