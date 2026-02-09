-- +goose Up
ALTER TABLE client_tokens ADD COLUMN partner_whatsapp TEXT;

-- +goose Down
ALTER TABLE client_tokens DROP COLUMN partner_whatsapp;
