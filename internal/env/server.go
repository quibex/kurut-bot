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

func newServers(ctx context.Context, cfg config.Config, logger *slog.Logger, clients *Clients, configStore *telegram.ConfigStore) *Servers {
	var servers Servers

	mux := http.NewServeMux()
	
	mux.HandleFunc("/wg/connect", telegram.WGConnectHandler(configStore))
	mux.HandleFunc("/wg/config/", telegram.WGConfigDownloadHandler(configStore))
	
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
