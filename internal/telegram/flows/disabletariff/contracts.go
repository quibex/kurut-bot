package disabletariff

import (
	"context"

	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type botApi interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type stateManager interface {
	SetState(tgUserID int64, state states.State, data interface{})
	Clear(tgUserID int64)
}

type tariffService interface {
	GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
	UpdateTariffStatus(ctx context.Context, tariffID int64, isActive bool) (*tariffs.Tariff, error)
}
