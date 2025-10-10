-- +goose Up
ALTER TABLE users ADD COLUMN language TEXT DEFAULT '' NOT NULL;

CREATE INDEX idx_users_language ON users(language);

-- +goose Down
DROP INDEX idx_users_language;
ALTER TABLE users DROP COLUMN language;


