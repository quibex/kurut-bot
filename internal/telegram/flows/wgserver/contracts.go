package wgserver

import (
	"context"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type botApi interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

type Storage interface {
	CreateWGServer(ctx context.Context, server storage.WGServer) (*storage.WGServer, error)
	GetWGServer(ctx context.Context, id int64) (*storage.WGServer, error)
	ListWGServers(ctx context.Context) ([]*storage.WGServer, error)
	UpdateWGServer(ctx context.Context, id int64, params map[string]interface{}) (*storage.WGServer, error)
	ArchiveWGServer(ctx context.Context, id int64) (*storage.WGServer, error)
	UnarchiveWGServer(ctx context.Context, id int64) (*storage.WGServer, error)
	DeleteWGServer(ctx context.Context, id int64) error
}

type StateManager interface {
	GetState(chatID int64) (string, interface{})
	SetState(chatID int64, state string, data interface{})
}

type TLSConfig interface {
	GetCACertPath() string
	GetClientCertPath() string
	GetClientKeyPath() string
	GetServerName() string
}
