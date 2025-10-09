-- +goose Up
ALTER TABLE users ADD COLUMN used_trial BOOLEAN DEFAULT FALSE;

CREATE INDEX idx_users_used_trial ON users(used_trial);

-- +goose Down
DROP INDEX idx_users_used_trial;
ALTER TABLE users DROP COLUMN used_trial;


