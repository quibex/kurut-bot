package retrysubscription

import (
	"context"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
)

type (
	// Storage provides database operations
	Storage interface {
		ListOrphanedPayments(ctx context.Context) ([]*payment.Payment, error)
		GetUser(ctx context.Context, criteria users.GetCriteria) (*users.User, error)
		ListTariffs(ctx context.Context, criteria tariffs.ListCriteria) ([]*tariffs.Tariff, error)
		ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error)
		IsSubscriptionLinkedToPayment(ctx context.Context, subscriptionID int64) (bool, error)
		LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error
	}

	// SubscriptionService provides subscription creation operations
	SubscriptionService interface {
		CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.Subscription, error)
	}

	// TelegramBot provides Telegram API operations
	TelegramBot interface {
		SendMessage(chatID int64, text string) error
	}
)
