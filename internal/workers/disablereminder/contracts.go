package disablereminder

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/subs"
)

// Storage provides database operations for stale expired subscriptions
type Storage interface {
	ListStaleExpiredSubscriptionsGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error)
}

// NotificationService provides notification functionality
type NotificationService interface {
	SendOverdueSubscriptionMessage(ctx context.Context, chatID int64, sub *subs.Subscription) error
}

// TelegramBot provides telegram messaging
type TelegramBot interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}
