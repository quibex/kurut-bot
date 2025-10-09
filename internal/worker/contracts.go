package worker

import (
	"context"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/users"
)

type (
	// Storage provides database operations for workers
	Storage interface {
		ListExpiringSubscriptions(ctx context.Context, daysUntilExpiry int) ([]*subs.Subscription, error)
		ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error)
		UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
		GetUser(ctx context.Context, criteria users.GetCriteria) (*users.User, error)
		GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error)
	}

	// TelegramBot provides Telegram API operations
	TelegramBot interface {
		SendMessage(chatID int64, text string) error
	}
)
