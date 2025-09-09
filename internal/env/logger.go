package environment

import (
	"log/slog"
	"os"
	"strings"

	"kurut-bot/internal/config"
)

func initLogger(cfg config.Config) (*slog.Logger, error) {
	var handler slog.Handler

	// Choose handler based on environment
	if cfg.Env == "local" {
		// Text handler for local development
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: parseLogLevel(cfg.Logger.Level),
		})
	} else {
		// JSON handler for production
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: parseLogLevel(cfg.Logger.Level),
		})
	}

	return slog.New(handler), nil
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
