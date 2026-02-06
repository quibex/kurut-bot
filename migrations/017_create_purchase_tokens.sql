-- +goose Up
-- +goose StatementBegin
CREATE TABLE purchase_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT NOT NULL UNIQUE,
    client_whatsapp TEXT NOT NULL,
    created_by_telegram_id INTEGER NOT NULL,
    tariff_id INTEGER,
    referrer_whatsapp TEXT,
    payment_id INTEGER,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'completed', 'cancelled')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (tariff_id) REFERENCES tariffs(id),
    FOREIGN KEY (payment_id) REFERENCES payments(id)
);

CREATE INDEX idx_purchase_tokens_token ON purchase_tokens(token);
CREATE INDEX idx_purchase_tokens_status ON purchase_tokens(status);
CREATE INDEX idx_purchase_tokens_payment_id ON purchase_tokens(payment_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_purchase_tokens_payment_id;
DROP INDEX IF EXISTS idx_purchase_tokens_status;
DROP INDEX IF EXISTS idx_purchase_tokens_token;
DROP TABLE IF EXISTS purchase_tokens;
-- +goose StatementEnd
