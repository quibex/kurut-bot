package lookup

import (
	"context"

	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/webtokens"
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
		GetLookupData(chatID int64) (*flows.LookupFlowData, error)
		SetState(chatID int64, state states.State, data any)
	}

	lookupStorage interface {
		SearchSubscriptionsByPhoneSuffix(ctx context.Context, suffix string) ([]storage.SubscriptionLookupResult, error)
	}

	clientTokenStorage interface {
		GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64, partnerWhatsApp *string) (*webtokens.ClientToken, error)
	}

	serverService interface {
		ListServers(ctx context.Context, criteria servers.ListCriteria) ([]*servers.Server, error)
	}

	subscriptionStorage interface {
		UpdateSubscriptionServer(ctx context.Context, subscriptionID int64, serverID int64) error
	}
)
