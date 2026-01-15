package createsubforclient

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"
)

type (
	botApi interface {
		Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
		Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	}

	stateManager interface {
		Clear(chatID int64)
		GetCreateSubForClientData(chatID int64) (*flows.CreateSubForClientFlowData, error)
		SetState(chatID int64, state states.State, data any)
	}

	tariffService interface {
		GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
		GetTrialTariff(ctx context.Context) (*tariffs.Tariff, error)
	}

	subscriptionService interface {
		CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.CreateSubscriptionResult, error)
		FindActiveSubscriptionByWhatsApp(ctx context.Context, whatsapp string) (*subs.Subscription, error)
	}

	subscriptionStorage interface {
		HasUsedTrialByPhone(ctx context.Context, phoneNumber string) (bool, error)
	}

	paymentService interface {
		CreatePayment(ctx context.Context, paymentEntity payment.Payment) (*payment.Payment, error)
		CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
		IsMockPayment() bool
	}

	orderService interface {
		CreatePendingOrder(ctx context.Context, order orders.PendingOrder) (*orders.PendingOrder, error)
		GetPendingOrderByID(ctx context.Context, id int64) (*orders.PendingOrder, error)
		UpdateMessageID(ctx context.Context, id int64, messageID int) error
		UpdatePaymentID(ctx context.Context, id int64, paymentID int64) error
		UpdateStatus(ctx context.Context, id int64, status orders.Status) error
		DeletePendingOrder(ctx context.Context, id int64) error
	}
)
