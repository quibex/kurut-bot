-- +goose Up
INSERT INTO tariffs (name, duration_days, price, is_active, created_at, updated_at)
VALUES ('Trial', 7, 0.0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

-- +goose Down
DELETE
FROM tariffs
WHERE name = 'Trial'
  AND duration_days = 7
  AND price = 0.0;
