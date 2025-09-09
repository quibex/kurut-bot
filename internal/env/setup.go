package environment

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/config"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"
)

type closer func()

type Env struct {
	Config   *config.Config
	Logger   *slog.Logger
	Servers  *Servers
	Clients  *Clients
	Services *Services

	Closers []closer
}

func Setup(ctx context.Context) (*Env, error) {
	// Загружаем .env файл если он существует (игнорируем ошибки - файл может не существовать)
	_ = godotenv.Load()

	var cfg config.Config
	err := envconfig.Process(ctx, &cfg)
	if err != nil {
		return nil, fmt.Errorf("env processing: %w", err)
	}

	var e Env

	logger, err := initLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("initLogger: %w", err)
	}

	clients, err := newClients(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("newClients: %w", err)
	}

	services, err := newServices(ctx, clients, &cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("newServices: %w", err)
	}

	servers := newServers(ctx, cfg, logger, clients)

	e.Servers = servers
	e.Config = &cfg
	e.Logger = logger
	e.Clients = clients
	e.Services = services
	e.Closers = []closer{} // Empty for now

	return &e, nil
}
