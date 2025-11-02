package healthcheck

import (
	"context"

	"kurut-bot/internal/storage"
)

type (
	Storage interface {
		ListEnabledWGServers(ctx context.Context) ([]*storage.WGServer, error)
	}

	TelegramNotifier interface {
		SendMessage(chatID int64, text string) error
	}
)

