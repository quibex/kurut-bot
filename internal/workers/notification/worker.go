package notification

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"

	"github.com/robfig/cron/v3"
)

const (
	NotificationType3Day = "3day"
	NotificationType1Day = "1day"
)

// Worker handles sending notifications for expiring subscriptions
type Worker struct {
	storage       Storage
	telegramBot   TelegramBot
	tariffStorage TariffStorage
	logger        *slog.Logger
	cron          *cron.Cron
}

// NewWorker creates a new notification worker
func NewWorker(
	storage Storage,
	telegramBot TelegramBot,
	tariffStorage TariffStorage,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		storage:       storage,
		telegramBot:   telegramBot,
		tariffStorage: tariffStorage,
		logger:        logger,
		cron:          cron.New(),
	}
}

// Name returns the worker name
func (w *Worker) Name() string {
	return "notification"
}

// Start starts the notification worker
func (w *Worker) Start() error {
	// Runs daily at 18:00
	_, err := w.cron.AddFunc("0 18 * * *", func() {
		ctx := context.Background()
		w.logger.Info("Running notification worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Notification worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule notification worker: %w", err)
	}

	w.cron.Start()
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping notification worker")
	w.cron.Stop()
}

// run executes the notification logic
func (w *Worker) run(ctx context.Context) error {
	w.logger.Info("Starting notification worker execution")

	// Process 3-day notifications
	if err := w.processExpiringSubscriptions(ctx, 3, NotificationType3Day); err != nil {
		w.logger.Error("Failed to process 3-day notifications", "error", err)
	}

	// Process 1-day notifications
	if err := w.processExpiringSubscriptions(ctx, 1, NotificationType1Day); err != nil {
		w.logger.Error("Failed to process 1-day notifications", "error", err)
	}

	w.logger.Info("Notification worker execution completed")
	return nil
}

// processExpiringSubscriptions processes subscriptions expiring in specified days
func (w *Worker) processExpiringSubscriptions(ctx context.Context, daysUntilExpiry int, notificationType string) error {
	w.logger.Info("Processing expiring subscriptions",
		"days", daysUntilExpiry,
		"notification_type", notificationType)

	subscriptions, err := w.storage.ListExpiringSubscriptions(ctx, daysUntilExpiry)
	if err != nil {
		return fmt.Errorf("list expiring subscriptions: %w", err)
	}

	w.logger.Info("Found expiring subscriptions",
		"count", len(subscriptions),
		"days", daysUntilExpiry)

	for _, sub := range subscriptions {
		if err := w.processSubscriptionNotification(ctx, sub, daysUntilExpiry, notificationType); err != nil {
			w.logger.Error("Failed to process subscription notification",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}
	}

	return nil
}

// processSubscriptionNotification processes notification for a single subscription
func (w *Worker) processSubscriptionNotification(ctx context.Context, sub *subs.Subscription, daysUntilExpiry int, notificationType string) error {
	// Get user to find telegram chat ID
	userID := sub.UserID
	user, err := w.storage.GetUser(ctx, users.GetCriteria{ID: &userID})
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found: %d", sub.UserID)
	}

	// Get tariff info
	tariffID := sub.TariffID
	tariff, err := w.tariffStorage.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil {
		w.logger.Warn("Failed to get tariff info",
			"tariff_id", sub.TariffID,
			"error", err)
	}

	// Send notification
	message := w.formatNotificationMessage(sub, tariff, daysUntilExpiry)
	if err := w.telegramBot.SendMessage(user.TelegramID, message); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}

	w.logger.Info("Notification sent successfully",
		"subscription_id", sub.ID,
		"user_id", sub.UserID,
		"telegram_id", user.TelegramID,
		"days_until_expiry", daysUntilExpiry)

	return nil
}

// formatNotificationMessage formats the notification message
func (w *Worker) formatNotificationMessage(sub *subs.Subscription, tariff *tariffs.Tariff, daysUntilExpiry int) string {
	var daysText string
	if daysUntilExpiry == 1 {
		daysText = "1 день"
	} else {
		daysText = fmt.Sprintf("%d дня", daysUntilExpiry)
	}

	tariffName := "подписка"
	if tariff != nil {
		tariffName = tariff.Name
	}

	expiresAtText := "неизвестно"
	if sub.ExpiresAt != nil {
		expiresAtText = sub.ExpiresAt.Format("02.01.2006 15:04")
	}

	return fmt.Sprintf(
		"⏰ Внимание! Ваша подписка истекает через %s\n\n"+
			"📅 Дата окончания: %s\n"+
			"🔑 Подключение: %s\n\n"+
			"Для продления подписки используйте команду /renew",
		daysText, expiresAtText, tariffName)
}



