-- +goose Up
ALTER TABLE subscriptions ADD COLUMN vpn_type TEXT DEFAULT 'marzban';
ALTER TABLE subscriptions ADD COLUMN vpn_data TEXT;

CREATE INDEX idx_subscriptions_vpn_type ON subscriptions(vpn_type);

-- +goose Down
DROP INDEX idx_subscriptions_vpn_type;
ALTER TABLE subscriptions DROP COLUMN vpn_data;
ALTER TABLE subscriptions DROP COLUMN vpn_type;

