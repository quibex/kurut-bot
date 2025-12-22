-- +goose Up
ALTER TABLE subscriptions
    ADD COLUMN last_renewed_at TIMESTAMP;

-- Initialize last_renewed_at with activated_at for existing subscriptions
UPDATE subscriptions
SET last_renewed_at = activated_at
WHERE activated_at IS NOT NULL;

-- For subscriptions without activated_at, use created_at
UPDATE subscriptions
SET last_renewed_at = created_at
WHERE last_renewed_at IS NULL;

CREATE INDEX idx_subscriptions_last_renewed_at ON subscriptions (last_renewed_at);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_last_renewed_at;
-- SQLite doesn't support DROP COLUMN, so we can't rollback this migration cleanly
