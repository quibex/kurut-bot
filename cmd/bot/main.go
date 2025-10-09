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
			logger.Info("Starting observability server", slog.String("addr", env.Servers.HTTP.Observability.Addr))
			if err := env.Servers.HTTP.Observability.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("Observability server error", slog.Any("error", err))
			}
		}()
	}

	// Запускаем Telegram бота
	if err := startTelegramBot(ctx, env); err != nil {
		logger.Error("Failed to start telegram bot", slog.Any("error", err))
		return
	}

	// Запускаем worker service
	if err := env.Services.WorkerService.Start(); err != nil {
		logger.Error("Failed to start worker service", slog.Any("error", err))
		return
	}

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("Bot started successfully. Press Ctrl+C to stop.")
	<-quit

	logger.Info("Shutting down application...")

	// Create context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), env.Config.ShutdownDuration)
	defer cancel()

	// Stop worker service
	if env.Services.WorkerService != nil {
		env.Services.WorkerService.Stop()
	}

	// Shutdown servers
	if env.Servers.HTTP.Observability != nil {
		if err := env.Servers.HTTP.Observability.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			logger.Error("Observability server shutdown error", slog.Any("error", err))
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
