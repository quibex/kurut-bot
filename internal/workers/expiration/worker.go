package expiration

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/subs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

// Worker handles sending notifications about expiring subscriptions
type Worker struct {
	storage             Storage
	telegramBot         TelegramBot
	notificationService NotificationService
	logger              *slog.Logger
	cron                *cron.Cron
}

// NewWorker creates a new expiration worker
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
	return "expiration"
}

// Start starts the expiration worker
func (w *Worker) Start() error {
	// Runs daily at 07:00
	_, err := w.cron.AddFunc("0 7 * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in expiration worker", "panic", r)
			}
		}()
		ctx := context.Background()
		w.logger.Info("Running expiration worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Expiration worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule expiration worker: %w", err)
	}

	w.cron.Start()
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping expiration worker")
	w.cron.Stop()
}

// RunNow runs the worker immediately (for manual testing)
func (w *Worker) RunNow(ctx context.Context) error {
	w.logger.Info("Manual run of expiration worker")
	return w.run(ctx)
}

// run executes the expiration logic
func (w *Worker) run(ctx context.Context) error {
	w.logger.Info("Starting expiration worker execution")

	// 1. Пометить истекшие как expired (до отправки уведомлений!)
	if err := w.markExpiredSubscriptions(ctx); err != nil {
		w.logger.Error("Failed to mark expired subscriptions", "error", err)
	}

	// 2. Уведомления о просроченных (status=expired)
	if err := w.sendOverdueNotifications(ctx); err != nil {
		w.logger.Error("Failed to send overdue notifications", "error", err)
	}

	w.logger.Info("Expiration worker execution completed")
	return nil
}

// sendOverdueNotifications sends notifications about overdue subscriptions
func (w *Worker) sendOverdueNotifications(ctx context.Context) error {
	overdueByAssistant, err := w.storage.ListOverdueSubscriptionsGroupedByAssistant(ctx)
	if err != nil {
		return fmt.Errorf("list overdue: %w", err)
	}

	w.logger.Info("Found overdue subscriptions", "assistants_count", len(overdueByAssistant))

	for assistantID, subscriptions := range overdueByAssistant {
		if err := w.sendOverdueNotification(ctx, assistantID, subscriptions); err != nil {
			w.logger.Error("Failed to send overdue notification",
				"assistant_id", assistantID,
				"error", err)
		}
	}

	return nil
}

// sendOverdueNotification sends a notification about overdue subscriptions to an assistant
func (w *Worker) sendOverdueNotification(ctx context.Context, assistantTelegramID int64, subscriptions []*subs.Subscription) error {
	if len(subscriptions) == 0 {
		return nil
	}

	// Summary message
	summaryText := fmt.Sprintf("⚠️ *У вас %d просроченных подписок*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions))
	summaryMsg := tgbotapi.NewMessage(assistantTelegramID, summaryText)
	summaryMsg.ParseMode = "Markdown"
	_, _ = w.telegramBot.Send(summaryMsg)

	// Individual messages via notification service
	for _, sub := range subscriptions {
		if err := w.notificationService.SendOverdueSubscriptionMessage(ctx, assistantTelegramID, sub); err != nil {
			w.logger.Error("Failed to send overdue subscription message", "error", err, "sub_id", sub.ID)
		}
	}

	return nil
}

// markExpiredSubscriptions marks expired subscriptions as expired in DB
func (w *Worker) markExpiredSubscriptions(ctx context.Context) error {
	subscriptions, err := w.storage.ListExpiredSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("list expired subscriptions: %w", err)
	}

	w.logger.Info("Marking expired subscriptions", "count", len(subscriptions))

	expiredStatus := subs.StatusExpired
	for _, sub := range subscriptions {
		criteria := subs.GetCriteria{IDs: []int64{sub.ID}}
		params := subs.UpdateParams{Status: &expiredStatus}

		_, err := w.storage.UpdateSubscription(ctx, criteria, params)
		if err != nil {
			w.logger.Error("Failed to expire subscription",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}

		w.logger.Info("Subscription expired",
			"subscription_id", sub.ID,
			"user_id", sub.UserID)
	}

	return nil
}
