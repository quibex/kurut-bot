package paymentautocheck

import (
	"context"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type (
	// OrderStorage provides operations for pending orders
	OrderStorage interface {
		ListPendingOrdersWithPayments(ctx context.Context) ([]*orders.PendingOrder, error)
		DeletePendingOrder(ctx context.Context, id int64) error
	}

	// MessageStorage provides operations for subscription messages
	MessageStorage interface {
		ListActiveMessagesWithPayments(ctx context.Context) ([]*submessages.SubscriptionMessage, error)
		DeactivateSubscriptionMessage(ctx context.Context, id int64) error
		GetSubscriptionMessageByID(ctx context.Context, id int64) (*submessages.SubscriptionMessage, error)
	}

	// PaymentService provides payment operations
	PaymentService interface {
		CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
		IsManualPayment() bool
	}

	// SubscriptionService provides subscription creation operations
	SubscriptionService interface {
		CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.CreateSubscriptionResult, error)
		MigrateSubscription(ctx context.Context, req *subs.MigrateSubscriptionRequest) (*subs.CreateSubscriptionResult, error)
	}

	// SubscriptionStorage provides subscription storage operations
	SubscriptionStorage interface {
		ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
		GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error)
		UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
		UpdateSubscriptionTariff(ctx context.Context, subscriptionID int64, tariffID int64) error
	}

	// TariffService provides tariff operations
	TariffService interface {
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	}

	// ServerStorage provides server operations
	ServerStorage interface {
		GetServer(ctx context.Context, criteria servers.GetCriteria) (*servers.Server, error)
	}

	// TelegramBot provides telegram messaging
	TelegramBot interface {
		Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	}
)
