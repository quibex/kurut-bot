package disablereminder

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/subs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

// Worker handles sending reminders about subscriptions that need to be disabled
type Worker struct {
	storage             Storage
	telegramBot         TelegramBot
	notificationService NotificationService
	logger              *slog.Logger
	cron                *cron.Cron
}

// NewWorker creates a new disable reminder worker
func NewWorker(
	storage Storage,
	telegramBot TelegramBot,
	notificationService NotificationService,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		storage:             storage,
		telegramBot:         telegramBot,
		notificationService: notificationService,
		logger:              logger,
		cron:                cron.New(),
	}
}

// Name returns the worker name
func (w *Worker) Name() string {
	return "disable-reminder"
}

// Start starts the disable reminder worker
func (w *Worker) Start() error {
	// Runs every hour at :00 minutes, starting from 8:00
	// This reminds assistants about subscriptions that expired >24h ago
	_, err := w.cron.AddFunc("0 8-23 * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in disable reminder worker", "panic", r)
			}
		}()
		ctx := context.Background()
		w.logger.Info("Running disable reminder worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Disable reminder worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule disable reminder worker: %w", err)
	}

	w.cron.Start()
	w.logger.Info("Disable reminder worker started", "schedule", "every hour 8:00-23:00")
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping disable reminder worker")
	w.cron.Stop()
}

// RunNow runs the worker immediately (for manual testing)
func (w *Worker) RunNow(ctx context.Context) error {
	w.logger.Info("Manual run of disable reminder worker")
	return w.run(ctx)
}

// run executes the reminder logic
func (w *Worker) run(ctx context.Context) error {
	w.logger.Info("Starting disable reminder worker execution")

	staleByAssistant, err := w.storage.ListStaleExpiredSubscriptionsGroupedByAssistant(ctx)
	if err != nil {
		return fmt.Errorf("list stale expired subscriptions: %w", err)
	}

	if len(staleByAssistant) == 0 {
		w.logger.Info("No stale expired subscriptions found")
		return nil
	}

	w.logger.Info("Found stale expired subscriptions", "assistants_count", len(staleByAssistant))

	for assistantID, subscriptions := range staleByAssistant {
		if err := w.sendReminderToAssistant(ctx, assistantID, subscriptions); err != nil {
			w.logger.Error("Failed to send disable reminder",
				"assistant_id", assistantID,
				"error", err)
		}
	}

	w.logger.Info("Disable reminder worker execution completed")
	return nil
}

// sendReminderToAssistant sends reminder notifications to an assistant
func (w *Worker) sendReminderToAssistant(ctx context.Context, assistantTelegramID int64, subscriptions []*subs.Subscription) error {
	if len(subscriptions) == 0 {
		return nil
	}

	// Summary message
	summaryText := fmt.Sprintf("🔴 *Напоминание: %d подписок ждут отключения*\n\n"+
		"Эти подписки истекли более 24 часов назад и требуют отключения на сервере.\n"+
		"Ниже отдельные сообщения для каждой подписки.", len(subscriptions))

	summaryMsg := tgbotapi.NewMessage(assistantTelegramID, summaryText)
	summaryMsg.ParseMode = "Markdown"
	if _, err := w.telegramBot.Send(summaryMsg); err != nil {
		w.logger.Error("Failed to send summary message", "error", err, "assistant_id", assistantTelegramID)
	}

	// Individual messages via notification service
	for _, sub := range subscriptions {
		if err := w.notificationService.SendOverdueSubscriptionMessage(ctx, assistantTelegramID, sub); err != nil {
			w.logger.Error("Failed to send overdue subscription message",
				"error", err,
				"sub_id", sub.ID)
		}
	}

	return nil
}
