package environment

import (
	"context"
	"kurut-bot/internal/config"
	"log/slog"
	"net/http"
)

type Servers struct {
	HTTP struct {
		Observability *http.Server
		API           *http.Server
	}
}

func newServers(ctx context.Context, cfg config.Config, logger *slog.Logger, clients *Clients) *Servers {
	var servers Servers

	// Simple API server - will be implemented later
	apiServer := &http.Server{
		Addr:    ":8080",            // Default port for now
		Handler: http.NewServeMux(), // Empty mux for now
	}

	servers.HTTP.API = apiServer
	servers.HTTP.Observability = initObservability(ctx, logger.WithGroup("http"), clients, cfg)

	return &servers
}
