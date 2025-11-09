package environment

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/config"
	"kurut-bot/internal/telegram"

	"github.com/joho/godotenv"
	"github.com/sethvargo/go-envconfig"
)

type closer func()

type Env struct {
	Config      *config.Config
	Logger      *slog.Logger
	Servers     *Servers
	Clients     *Clients
	Services    *Services
	ConfigStore *telegram.ConfigStore

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

	if err := cfg.WireGuard.PrepareCertFiles(); err != nil {
		return nil, fmt.Errorf("prepare TLS certificates: %w", err)
	}
	logger.Info("TLS certificates prepared",
		"ca_cert", cfg.WireGuard.GetCACertPath(),
		"client_cert", cfg.WireGuard.GetClientCertPath(),
		"client_key", cfg.WireGuard.GetClientKeyPath(),
		"server_name", cfg.WireGuard.TLSServerName)

	clients, err := newClients(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("newClients: %w", err)
	}

	configStore := telegram.NewConfigStore()

	services, err := newServices(ctx, clients, &cfg, logger, configStore)
	if err != nil {
		return nil, fmt.Errorf("newServices: %w", err)
	}

	servers := newServers(ctx, cfg, logger, clients, configStore)

	e.Servers = servers
	e.Config = &cfg
	e.Logger = logger
	e.Clients = clients
	e.Services = services
	e.ConfigStore = configStore
	e.Closers = []closer{} // Empty for now

	return &e, nil
}
