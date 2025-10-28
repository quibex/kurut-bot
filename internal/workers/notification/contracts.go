package notification

import (
	"context"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
)

type (
	// Storage provides database operations
	Storage interface {
		ListExpiringSubscriptions(ctx context.Context, daysUntilExpiry int) ([]*subs.Subscription, error)
		GetUser(ctx context.Context, criteria users.GetCriteria) (*users.User, error)
	}

	// TariffStorage provides tariff operations
	TariffStorage interface {
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	}

	// TelegramBot provides Telegram API operations
	TelegramBot interface {
		SendMessage(chatID int64, text string) error
	}
)




