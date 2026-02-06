package environment

import (
	"context"
	"kurut-bot/internal/config"
	"kurut-bot/internal/telegram"
	"log/slog"
	"net/http"
)

type Servers struct {
	HTTP struct {
		Observability *http.Server
		API           *http.Server
	}
}

func newServers(ctx context.Context, cfg config.Config, logger *slog.Logger, clients *Clients, configStore *telegram.ConfigStore, services *Services) *Servers {
	var servers Servers

	mux := http.NewServeMux()

	// WireGuard endpoints
	mux.HandleFunc("/wg/connect", telegram.WGConnectHandler(configStore))
	mux.HandleFunc("/wg/config/", telegram.WGConfigDownloadHandler(configStore))

	// Web payment endpoints
	// New unified client page
	mux.HandleFunc("/c/", services.WebHandlers.ClientPageHandler())

	// Static files
	mux.HandleFunc("/static/kurut.jpg", services.WebHandlers.StaticHandler("kurut.jpg"))

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	apiServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	servers.HTTP.API = apiServer
	servers.HTTP.Observability = initObservability(ctx, logger.WithGroup("http"), clients, cfg)

	return &servers
}
