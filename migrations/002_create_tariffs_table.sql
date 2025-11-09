-- +goose Up
CREATE TABLE tariffs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    duration_days INTEGER NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    traffic_limit_gb INTEGER,
    is_active BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_tariffs_is_active ON tariffs(is_active);
CREATE INDEX idx_tariffs_price ON tariffs(price);

-- +goose Down
DROP TABLE tariffs;


