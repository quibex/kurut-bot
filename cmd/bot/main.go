package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http" // add this import for http.ErrServerClosed
	"os"
	"os/signal"
	"syscall"

	environment "kurut-bot/internal/env"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("FATAL PANIC in main: %v", r)
			panic(r)
		}
	}()

	ctx := context.Background()

	// Initialize environment
	env, err := environment.Setup(ctx)
	if err != nil {
		log.Fatalf("Failed to setup environment: %v", err)
	}

	logger := env.Logger
	logger.Info("Starting kurut-bot application")

	// Start observability server in background
	if env.Servers.HTTP.Observability != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Panic in observability server goroutine", slog.Any("panic", r))
				}
			}()
			logger.Info("Starting observability server", slog.String("addr", env.Servers.HTTP.Observability.Addr))
			if err := env.Servers.HTTP.Observability.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("Observability server error", slog.Any("error", err))
			}
		}()
	}

	// Start API server in background
	if env.Servers.HTTP.API != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Panic in API server goroutine", slog.Any("panic", r))
				}
			}()
			logger.Info("Starting API server", slog.String("addr", env.Servers.HTTP.API.Addr))
			if err := env.Servers.HTTP.API.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("API server error", slog.Any("error", err))
			}
		}()
	}

	// Запускаем Telegram бота
	logger.Info("Starting Telegram bot...")
	if err := startTelegramBot(ctx, env); err != nil {
		logger.Error("Failed to start telegram bot", slog.Any("error", err))
		log.Fatalf("FATAL: Failed to start telegram bot: %v", err)
	}
	logger.Info("Telegram bot started successfully")

	// Запускаем worker manager
	logger.Info("Starting worker manager...")
	if err := env.Services.WorkerManager.Start(); err != nil {
		logger.Error("Failed to start worker manager", slog.Any("error", err))
		log.Fatalf("FATAL: Failed to start worker manager: %v", err)
	}
	logger.Info("Worker manager started successfully")

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("Bot started successfully. Press Ctrl+C to stop.")
	<-quit

	logger.Info("Shutting down application...")

	// Create context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), env.Config.ShutdownDuration)
	defer cancel()

	// Stop worker manager
	if env.Services.WorkerManager != nil {
		env.Services.WorkerManager.Stop()
	}

	// Shutdown servers
	if env.Servers.HTTP.Observability != nil {
		if err := env.Servers.HTTP.Observability.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			logger.Error("Observability server shutdown error", slog.Any("error", err))
		}
	}

	if env.Servers.HTTP.API != nil {
		if err := env.Servers.HTTP.API.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			logger.Error("API server shutdown error", slog.Any("error", err))
		}
	}

	// Close resources
	for _, closer := range env.Closers {
		closer()
	}

	logger.Info("Application stopped")
}

func startTelegramBot(ctx context.Context, env *environment.Env) error {
	logger := env.Logger

	// Проверяем что telegram клиент инициализирован
	if env.Clients.TelegramBot == nil {
		logger.Error("Telegram bot не инициализирован - проверьте TELEGRAM_TOKEN")
		return fmt.Errorf("telegram bot не инициализирован")
	}

	// Проверяем что роутер инициализирован
	if env.Services.TelegramRouter == nil {
		logger.Error("Telegram router не инициализирован")
		return fmt.Errorf("telegram router не инициализирован")
	}

	// Запускаем telegram клиент
	if err := env.Clients.TelegramBot.Start(ctx); err != nil {
		return fmt.Errorf("запуск telegram клиента: %w", err)
	}

	// Устанавливаем команды для меню бота
	if err := env.Services.TelegramRouter.SetupBotCommands(); err != nil {
		logger.Error("Failed to setup bot commands", slog.Any("error", err))
		// Не возвращаем ошибку, т.к. это не критично
	} else {
		logger.Info("Bot commands set up successfully")
	}

	// Получаем канал обновлений
	updates := env.Clients.TelegramBot.GetUpdates()

	logger.Info("Started listening for updates with router...")

	// Запускаем роутер для обработки обновлений
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic in telegram bot goroutine", slog.Any("panic", r))
			}
		}()
		for {
			select {
			case <-ctx.Done():
				env.Clients.TelegramBot.Stop()
				return
			case update := <-updates:
				// Логируем входящие обновления
				if update.Message != nil {
					logger.Info("Получено сообщение",
						slog.Int64("chat_id", update.Message.Chat.ID),
						slog.Int64("user_id", update.Message.From.ID),
						slog.String("text", update.Message.Text))
				} else if update.CallbackQuery != nil {
					logger.Info("Получен callback",
						slog.Int64("chat_id", update.CallbackQuery.Message.Chat.ID),
						slog.Int64("user_id", update.CallbackQuery.From.ID),
						slog.String("data", update.CallbackQuery.Data))
				}

				// Обрабатываем через роутер
				if err := env.Services.TelegramRouter.Route(&update); err != nil {
					logger.Error("Ошибка обработки обновления", slog.Any("error", err))
				}
			}
		}
	}()

	return nil
}
