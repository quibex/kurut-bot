-- +goose Up
UPDATE tariffs
SET duration_days = 3,
    updated_at    = CURRENT_TIMESTAMP
WHERE name = 'Trial'
  AND price = 0.0;

-- +goose Down
UPDATE tariffs
SET duration_days = 7,
    updated_at    = CURRENT_TIMESTAMP
WHERE name = 'Trial'
  AND price = 0.0;
