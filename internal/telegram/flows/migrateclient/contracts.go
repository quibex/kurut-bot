package migrateclient

import (
	"context"

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
		GetMigrateClientData(chatID int64) (*flows.MigrateClientFlowData, error)
		SetState(chatID int64, state states.State, data any)
	}

	serverService interface {
		ListServers(ctx context.Context, criteria servers.ListCriteria) ([]*servers.Server, error)
	}

	serverStorage interface {
		GetServer(ctx context.Context, criteria servers.GetCriteria) (*servers.Server, error)
	}

	clientTokenStorage interface {
		GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64, partnerWhatsApp *string) (*webtokens.ClientToken, error)
	}
)
