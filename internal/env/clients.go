package environment

import (
	"context"
	"log/slog"
	"time"

	"kurut-bot/internal/config"
	"kurut-bot/internal/infra/sqlite3"
	"kurut-bot/internal/infra/telegram"
	"kurut-bot/pkg/marzban"

	"github.com/pkg/errors"
)

type Clients struct {
	SQLiteDB      *sqlite3.DB
	MarzbanClient marzban.Invoker
	TelegramBot   *telegram.Client
}

// tokenSecuritySource provides Bearer token authentication for Marzban client
type tokenSecuritySource struct {
	token string
}

// OAuth2PasswordBearer implements SecuritySource interface
func (s *tokenSecuritySource) OAuth2PasswordBearer(ctx context.Context, operationName marzban.OperationName) (marzban.OAuth2PasswordBearer, error) {
	return marzban.OAuth2PasswordBearer{
		Token:  s.token,
		Scopes: []string{}, // Empty scopes for now
	}, nil
}

func newClients(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Clients, error) {
	sqliteDB, err := provideSQLiteDB(ctx, cfg)
	if err != nil {
		return nil, err
	}

	marzbanClient, err := provideMarzbanClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	telegramBot, err := provideTelegramBot(cfg, logger)
	if err != nil {
		return nil, err
	}

	return &Clients{
		SQLiteDB:      sqliteDB,
		MarzbanClient: marzbanClient,
		TelegramBot:   telegramBot,
	}, nil
}

func provideMarzbanClient(ctx context.Context, cfg config.Config) (marzban.Invoker, error) {
	// Check if token is provided
	if cfg.MarzbanClient.Token == "" {
		// Return nil client if no token provided (will be handled gracefully)
		return nil, errors.New("marzban token is not provided")
	}
	if cfg.MarzbanClient.APIURL == "" {
		return nil, errors.New("marzban address is not provided")
	}

	// Create security source with token
	sec := &tokenSecuritySource{
		token: cfg.MarzbanClient.Token,
	}

	// Create Marzban client
	serverURL := cfg.MarzbanClient.APIURL
	client, err := marzban.NewClient(serverURL, sec)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func provideSQLiteDB(ctx context.Context, cfg config.Config) (*sqlite3.DB, error) {
	// Parse max lifetime from string to duration, use default if empty
	maxLifetimeStr := cfg.DB.MaxLifetime
	if maxLifetimeStr == "" {
		maxLifetimeStr = "5m" // default value
	}
	maxLifetime, err := time.ParseDuration(maxLifetimeStr)
	if err != nil {
		return nil, err
	}

	// Create SQLite DB with options from config
	opts := []sqlite3.Option{
		sqlite3.WithDSN(cfg.DB.Path),
		sqlite3.WithMaxOpenConns(cfg.DB.MaxOpenConns),
		sqlite3.WithMaxIdleConns(cfg.DB.MaxIdleConns),
		sqlite3.WithConnMaxLifetime(maxLifetime),
	}

	return sqlite3.New(ctx, opts...)
}

func provideTelegramBot(cfg config.Config, logger *slog.Logger) (*telegram.Client, error) {
	// Check if token is provided
	if cfg.Telegram.BotToken == "" {
		// Return nil client if no token provided (will be handled gracefully)
		return nil, nil
	}

	// Create telegram client
	client, err := telegram.NewClient(cfg.Telegram.BotToken, logger)
	if err != nil {
		return nil, err
	}

	return client, nil
}
