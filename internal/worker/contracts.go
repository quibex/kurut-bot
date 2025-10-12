package worker

import (
	"context"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
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
		ListOrphanedPayments(ctx context.Context) ([]*payment.Payment, error)
		GetPayment(ctx context.Context, criteria payment.GetCriteria) (*payment.Payment, error)
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
		ListTariffs(ctx context.Context, criteria tariffs.ListCriteria) ([]*tariffs.Tariff, error)
	}

	// TelegramBot provides Telegram API operations
	TelegramBot interface {
		SendMessage(chatID int64, text string) error
	}

	// SubscriptionService provides subscription creation operations
	SubscriptionService interface {
		CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.Subscription, error)
	}

	// Localizer provides localization operations
	Localizer interface {
		Get(lang, key string, data map[string]interface{}) string
	}
)
