package renewsub

import (
	"context"

	"kurut-bot/internal/stories/payment"
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
		SetState(chatID int64, state states.State, data any)
		GetRenewSubData(chatID int64) (*flows.RenewSubFlowData, error)
		Clear(chatID int64)
	}

	subscriptionService interface {
		GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error)
		ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error)
		ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
	}

	tariffService interface {
		GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	}

	paymentService interface {
		CreatePayment(ctx context.Context, payment payment.Payment) (*payment.Payment, error)
		CheckPaymentStatus(ctx context.Context, paymentID int64) (*payment.Payment, error)
		LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error
	}

	localizer interface {
		Get(lang, key string, params map[string]interface{}) string
	}
)
