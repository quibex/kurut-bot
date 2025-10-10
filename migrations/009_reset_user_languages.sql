-- +goose Up
-- Обнуляем язык у всех пользователей, чтобы они выбрали его заново
UPDATE users SET language = '' WHERE language IS NOT NULL;

-- +goose Down
-- Откатываем изменения - устанавливаем русский по умолчанию
UPDATE users SET language = 'ru' WHERE language = '';


