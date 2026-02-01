package expiration

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/subs"
)

type (
	// Storage provides database operations
	Storage interface {
		ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error)
		ListOverdueSubscriptionsGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error)
		UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
	}

	// NotificationService provides notification functionality
	NotificationService interface {
		SendOverdueSubscriptionMessage(ctx context.Context, chatID int64, sub *subs.Subscription) error
	}

	TelegramBot interface {
		Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	}
)
