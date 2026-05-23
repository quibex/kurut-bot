package expiringnotify

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/subs"
)

// Storage provides database operations for expiring subscriptions
type Storage interface {
	ListExpiringSubscriptions(ctx context.Context, daysUntilExpiry int) ([]*subs.Subscription, error)
}

// TelegramBot provides telegram messaging
type TelegramBot interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}
