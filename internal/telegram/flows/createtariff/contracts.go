package createtariff

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

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
		GetState(chatID int64) states.State
		SetState(chatID int64, state states.State, data any)
		Clear(chatID int64)
		GetCreateTariffData(chatID int64) (*flows.CreateTariffFlowData, error)
	}

	tariffService interface {
		CreateTariff(ctx context.Context, tariff tariffs.Tariff) (*tariffs.Tariff, error)
	}
)
