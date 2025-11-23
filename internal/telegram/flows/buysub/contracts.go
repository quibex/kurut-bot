package buysub

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/storage"
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
		GetBuySubData(chatID int64) (*flows.BuySubFlowData, error)
		SetState(chatID int64, state states.State, data any)
	}

	tariffService interface {
		GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
	}

	subscriptionService interface {
		CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.Subscription, error)
	}

	paymentService interface {
		CreatePayment(ctx context.Context, paymentEntity payment.Payment) (*payment.Payment, error)
		CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
	}

	storageService interface {
		ListEnabledWGServers(ctx context.Context) ([]*storage.WGServer, error)
	}

	localizer interface {
		Get(lang, key string, params map[string]interface{}) string
	}

	configStore interface {
		Store(config string, qrCode string) string
	}
)
