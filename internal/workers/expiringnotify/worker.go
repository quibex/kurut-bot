package expiringnotify

import (
	"context"
	"fmt"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

// Worker sends daily summary notifications about expiring subscriptions to assistants.
// Instead of individual messages per subscription, it sends one summary per assistant
// with a "Today's Expiring" button that triggers the /expiring command.
type Worker struct {
	storage     Storage
	telegramBot TelegramBot
	logger      *slog.Logger
	cron        *cron.Cron
}

func NewWorker(
	storage Storage,
	telegramBot TelegramBot,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		storage:     storage,
		telegramBot: telegramBot,
		logger:      logger,
		cron:        cron.New(),
	}
}

func (w *Worker) Name() string {
	return "expiring-notify"
}

func (w *Worker) Start() error {
	// 09:00 Moscow time daily
	_, err := w.cron.AddFunc("CRON_TZ=Europe/Moscow 0 9 * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in expiring-notify worker", "panic", r)
			}
		}()
		ctx := context.Background()
		w.logger.Info("Running expiring-notify worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Expiring-notify worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule expiring-notify worker: %w", err)
	}

	w.cron.Start()
	w.logger.Info("Expiring-notify worker started", "schedule", "daily at 09:00 MSK")
	return nil
}

func (w *Worker) Stop() {
	w.logger.Info("Stopping expiring-notify worker")
	w.cron.Stop()
}

func (w *Worker) RunNow(ctx context.Context) error {
	w.logger.Info("Manual run of expiring-notify worker")
	return w.run(ctx)
}

func (w *Worker) run(ctx context.Context) error {
	subscriptions, err := w.storage.ListExpiringSubscriptions(ctx, 0) // 0 = today
	if err != nil {
		return fmt.Errorf("list expiring subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		w.logger.Info("No expiring subscriptions today")
		return nil
	}

	// Group by assistant
	byAssistant := make(map[int64]int)
	for _, sub := range subscriptions {
		if sub.CreatedByTelegramID != nil {
			byAssistant[*sub.CreatedByTelegramID]++
		}
	}

	w.logger.Info("Sending expiring summary notifications",
		"total_subs", len(subscriptions),
		"assistants", len(byAssistant))

	for assistantID, count := range byAssistant {
		text := fmt.Sprintf("⏰ Сегодня истекает *%d подписок*", count)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📋 Сегодня истекает", "exp_today"),
			),
		)

		msg := tgbotapi.NewMessage(assistantID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard

		if _, err := w.telegramBot.Send(msg); err != nil {
			w.logger.Error("Failed to send expiring summary",
				"assistant_id", assistantID,
				"error", err)
		} else {
			w.logger.Info("Sent expiring summary",
				"assistant_id", assistantID,
				"count", count)
		}
	}

	return nil
}
