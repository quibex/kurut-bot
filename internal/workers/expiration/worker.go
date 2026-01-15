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

	// 1. Уведомления за 3 дня
	if err := w.sendExpiringNotifications(ctx, 3); err != nil {
		w.logger.Error("Failed to send 3-day notifications", "error", err)
	}

	// 2. Уведомления в день истечения
	if err := w.sendExpiringNotifications(ctx, 0); err != nil {
		w.logger.Error("Failed to send expiring today notifications", "error", err)
	}

	// 3. Уведомления о просроченных
	if err := w.sendOverdueNotifications(ctx); err != nil {
		w.logger.Error("Failed to send overdue notifications", "error", err)
	}

	// 4. Пометить истекшие как expired
	if err := w.markExpiredSubscriptions(ctx); err != nil {
		w.logger.Error("Failed to mark expired subscriptions", "error", err)
	}

	w.logger.Info("Expiration worker execution completed")
	return nil
}

// sendExpiringNotifications отправляет уведомления за N дней до истечения
func (w *Worker) sendExpiringNotifications(ctx context.Context, daysUntilExpiry int) error {
	expiringByAssistant, err := w.storage.ListExpiringByAssistantAndDays(ctx, daysUntilExpiry)
	if err != nil {
		return fmt.Errorf("list expiring subscriptions for %d days: %w", daysUntilExpiry, err)
	}

	w.logger.Info("Found expiring subscriptions",
		"assistants_count", len(expiringByAssistant),
		"days_until_expiry", daysUntilExpiry)

	for assistantID, subscriptions := range expiringByAssistant {
		if err := w.sendExpiringNotificationToAssistant(ctx, assistantID, subscriptions, daysUntilExpiry); err != nil {
			w.logger.Error("Failed to send expiring notification",
				"assistant_id", assistantID,
				"days_until_expiry", daysUntilExpiry,
				"error", err)
		}
	}

	return nil
}

// sendExpiringNotificationToAssistant отправляет уведомления об истекающих подписках ассистенту
func (w *Worker) sendExpiringNotificationToAssistant(
	ctx context.Context,
	assistantTelegramID int64,
	subscriptions []*subs.Subscription,
	daysUntilExpiry int,
) error {
	if len(subscriptions) == 0 {
		return nil
	}

	// Формируем сводное сообщение
	var summaryText string
	switch daysUntilExpiry {
	case 0:
		summaryText = fmt.Sprintf("🔔 *У вас %d подписок истекают сегодня*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions))
	case 3:
		summaryText = fmt.Sprintf("⏰ *У вас %d подписок истекают через 3 дня*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions))
	default:
		summaryText = fmt.Sprintf("⏰ *У вас %d подписок истекают через %d дней*\n\nНиже отдельные сообщения для каждой подписки.", len(subscriptions), daysUntilExpiry)
	}

	summaryMsg := tgbotapi.NewMessage(assistantTelegramID, summaryText)
	summaryMsg.ParseMode = "Markdown"
	_, _ = w.telegramBot.Send(summaryMsg)

	// Отправляем отдельные сообщения через notification service
	for _, sub := range subscriptions {
		if err := w.notificationService.SendExpiringSubscriptionMessage(ctx, assistantTelegramID, sub, daysUntilExpiry); err != nil {
			w.logger.Error("Failed to send expiring subscription message",
				"error", err,
				"sub_id", sub.ID,
				"days_until_expiry", daysUntilExpiry)
		}
	}

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
