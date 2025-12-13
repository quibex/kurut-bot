-- +goose Up
CREATE TABLE pending_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    payment_id INTEGER NOT NULL,
    admin_user_id INTEGER NOT NULL,
    assistant_telegram_id INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    message_id INTEGER,
    client_whatsapp TEXT NOT NULL,
    tariff_id INTEGER NOT NULL,
    tariff_name TEXT NOT NULL,
    total_amount DECIMAL(10,2) NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'cancelled')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_pending_orders_payment_id ON pending_orders(payment_id);
CREATE INDEX idx_pending_orders_status ON pending_orders(status);

-- +goose Down
DROP TABLE pending_orders;
