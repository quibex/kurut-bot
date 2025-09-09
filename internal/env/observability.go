package environment

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"

	"kurut-bot/internal/config"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func initObservability(
	_ context.Context,
	_ *slog.Logger,
	clients *Clients,
	cfg config.Config,
) *http.Server {
	mux := http.NewServeMux()

	// pprof endpoints
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// simple health checks
	mux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Add actual readiness checks when clients are implemented
		// For now, just return ready if we have clients
		_ = clients // Suppress unused variable warning

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Ready")
	})

	return &http.Server{
		Handler:           mux,
		Addr:              cfg.Observability.ADDR(),
		ReadTimeout:       cfg.Observability.ReadTimeout,
		WriteTimeout:      cfg.Observability.WriteTimeout,
		IdleTimeout:       cfg.Observability.IdleTimeout,
		ReadHeaderTimeout: cfg.Observability.ReadTimeout,
	}
}
