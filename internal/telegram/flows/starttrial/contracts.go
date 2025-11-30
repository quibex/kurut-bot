package starttrial

import (
	"context"

	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type botApi interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type tariffService interface {
	GetTrialTariff(ctx context.Context) (*tariffs.Tariff, error)
}

type subscriptionService interface {
	CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.Subscription, error)
}

type userService interface {
	MarkTrialAsUsed(ctx context.Context, userID int64) error
}

type localStorage interface {
	ListEnabledWGServers(ctx context.Context) ([]*storage.WGServer, error)
}

type configStore interface {
	Store(config string, qrCode string) string
}
