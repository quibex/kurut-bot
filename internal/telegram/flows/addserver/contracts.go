package addserver

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"
)

type (
	botApi interface {
		Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
		Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	}

	stateManager interface {
		SetState(chatID int64, state states.State, data interface{})
		GetAddServerData(chatID int64) (*flows.AddServerFlowData, error)
		Clear(chatID int64)
	}

	serverService interface {
		CreateServer(ctx context.Context, server servers.Server) (*servers.Server, error)
	}
)
