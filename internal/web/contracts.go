package web

import (
	"context"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/webtokens"
)

type TariffService interface {
	GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
}

type PaymentService interface {
	CreatePayment(ctx context.Context, paymentEntity payment.Payment) (*payment.Payment, error)
	CreatePaymentWithReturnURL(ctx context.Context, paymentEntity payment.Payment, returnURL string) (*payment.Payment, error)
	CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
	CancelPayment(ctx context.Context, paymentID int64) error
}

type PurchaseTokenStorage interface {
	GetPurchaseTokenByToken(ctx context.Context, token string) (*webtokens.PurchaseToken, error)
	UpdatePurchaseToken(ctx context.Context, id int64, tariffID int64, referrerWhatsApp *string, paymentID int64) error
	UpdatePurchaseTokenStatus(ctx context.Context, id int64, status webtokens.PurchaseTokenStatus) error
}

type RenewalTokenStorage interface {
	GetRenewalTokenByToken(ctx context.Context, token string) (*webtokens.RenewalToken, error)
}

type SubscriptionStorage interface {
	GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error)
	ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error)
}

type SubscriptionMessageStorage interface {
	CreateSubscriptionMessageWithPayment(ctx context.Context, subscriptionID int64, tariffID int64, paymentID int64) error
	CancelActiveMessagesWithPayments(ctx context.Context, subscriptionID int64) ([]int64, error)
	ListActiveSubscriptionMessages(ctx context.Context, subscriptionID int64) ([]*submessages.SubscriptionMessage, error)
	DeactivateSubscriptionMessage(ctx context.Context, id int64) error
}

type ClientTokenStorage interface {
	GetClientTokenByToken(ctx context.Context, token string) (*webtokens.ClientToken, error)
}

type OrderStorage interface {
	CreatePendingOrderFromWeb(ctx context.Context, order PendingOrder) (*PendingOrder, error)
	CancelPendingOrdersByWhatsApp(ctx context.Context, whatsapp string) ([]int64, error) // returns cancelled payment IDs
}

type ServerStorage interface {
	GetAvailableServer(ctx context.Context) (*servers.Server, error)
}

// PendingOrder for new subscriptions
type PendingOrder struct {
	ID                  int64
	ClientWhatsApp      string
	TariffID            int64
	ReferrerWhatsApp    *string
	PaymentID           int64
	CreatedByTelegramID int64
}
