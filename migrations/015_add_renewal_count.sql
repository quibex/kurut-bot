-- +goose Up
ALTER TABLE subscriptions
    ADD COLUMN renewal_count INTEGER DEFAULT 0;

-- Initialize for existing subscriptions based on last_renewed_at vs created_at
UPDATE subscriptions
SET renewal_count = CASE
                        WHEN last_renewed_at > created_at THEN 1
                        ELSE 0
    END;

CREATE INDEX idx_subscriptions_renewal_count ON subscriptions (renewal_count);

-- +goose Down
DROP INDEX IF EXISTS idx_subscriptions_renewal_count;
-- Note: SQLite doesn't support DROP COLUMN directly. The column will remain if downgrading.
