package migrateclient

import (
	"context"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type (
	botApi interface {
		Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
		Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	}

	stateManager interface {
		Clear(chatID int64)
		GetMigrateClientData(chatID int64) (*flows.MigrateClientFlowData, error)
		SetState(chatID int64, state states.State, data any)
	}

	tariffService interface {
		GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
	}

	serverService interface {
		ListServers(ctx context.Context, criteria servers.ListCriteria) ([]*servers.Server, error)
	}

	subscriptionService interface {
		MigrateSubscription(ctx context.Context, req *subs.MigrateSubscriptionRequest) (*subs.CreateSubscriptionResult, error)
	}

	paymentService interface {
		CreatePayment(ctx context.Context, paymentEntity payment.Payment) (*payment.Payment, error)
		CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
	}

	orderService interface {
		CreatePendingOrder(ctx context.Context, order orders.PendingOrder) (*orders.PendingOrder, error)
		GetPendingOrderByID(ctx context.Context, id int64) (*orders.PendingOrder, error)
		UpdateMessageID(ctx context.Context, id int64, messageID int) error
		UpdatePaymentID(ctx context.Context, id int64, paymentID int64) error
		DeletePendingOrder(ctx context.Context, id int64) error
	}
)
