package worker

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

type Service struct {
	storage       Storage
	telegramBot   TelegramBot
	tariffStorage tariffStorage
	logger        *slog.Logger
	cron          *cron.Cron
}

type tariffStorage interface {
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
}

func NewService(storage Storage, telegramBot TelegramBot, tariffStorage tariffStorage, logger *slog.Logger) *Service {
	return &Service{
		storage:       storage,
		telegramBot:   telegramBot,
		tariffStorage: tariffStorage,
		logger:        logger,
		cron:          cron.New(),
	}
}

// Start starts the cron workers
func (s *Service) Start() error {
	s.logger.Info("Starting worker service")

	// Notification worker: runs daily at 18:00
	_, err := s.cron.AddFunc("0 18 * * *", func() {
		ctx := context.Background()
		s.logger.Info("Running notification worker")
		if err := s.runNotificationWorker(ctx); err != nil {
			s.logger.Error("Notification worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add notification worker: %w", err)
	}

	// Expiration worker: runs daily at 00:10
	_, err = s.cron.AddFunc("10 0 * * *", func() {
		ctx := context.Background()
		s.logger.Info("Running expiration worker")
		if err := s.runExpirationWorker(ctx); err != nil {
			s.logger.Error("Expiration worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add expiration worker: %w", err)
	}

	s.cron.Start()
	s.logger.Info("Worker service started successfully")

	return nil
}

// Stop stops the cron workers
func (s *Service) Stop() {
	s.logger.Info("Stopping worker service")
	s.cron.Stop()
	s.logger.Info("Worker service stopped")
}

// runNotificationWorker sends notifications for expiring subscriptions
func (s *Service) runNotificationWorker(ctx context.Context) error {
	s.logger.Info("Starting notification worker execution")

	// Process 3-day notifications
	if err := s.processExpiringSubscriptions(ctx, 3, NotificationType3Day); err != nil {
		s.logger.Error("Failed to process 3-day notifications", "error", err)
	}

	// Process 1-day notifications
	if err := s.processExpiringSubscriptions(ctx, 1, NotificationType1Day); err != nil {
		s.logger.Error("Failed to process 1-day notifications", "error", err)
	}

	s.logger.Info("Notification worker execution completed")
	return nil
}

// processExpiringSubscriptions processes subscriptions expiring in specified days
func (s *Service) processExpiringSubscriptions(ctx context.Context, daysUntilExpiry int, notificationType string) error {
	s.logger.Info("Processing expiring subscriptions",
		"days", daysUntilExpiry,
		"notification_type", notificationType)

	subscriptions, err := s.storage.ListExpiringSubscriptions(ctx, daysUntilExpiry)
	if err != nil {
		return fmt.Errorf("list expiring subscriptions: %w", err)
	}

	s.logger.Info("Found expiring subscriptions",
		"count", len(subscriptions),
		"days", daysUntilExpiry)

	for _, sub := range subscriptions {
		if err := s.processSubscriptionNotification(ctx, sub, daysUntilExpiry, notificationType); err != nil {
			s.logger.Error("Failed to process subscription notification",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}
	}

	return nil
}

// processSubscriptionNotification processes notification for a single subscription
func (s *Service) processSubscriptionNotification(ctx context.Context, sub *subs.Subscription, daysUntilExpiry int, notificationType string) error {
	// Get user to find telegram chat ID
	userID := sub.UserID
	user, err := s.storage.GetUser(ctx, users.GetCriteria{ID: &userID})
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found: %d", sub.UserID)
	}

	// Get tariff info
	tariffID := sub.TariffID
	tariff, err := s.tariffStorage.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil {
		s.logger.Warn("Failed to get tariff info",
			"tariff_id", sub.TariffID,
			"error", err)
	}

	// Send notification
	message := s.formatNotificationMessage(sub, tariff, daysUntilExpiry)
	if err := s.telegramBot.SendMessage(user.TelegramID, message); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}

	s.logger.Info("Notification sent successfully",
		"subscription_id", sub.ID,
		"user_id", sub.UserID,
		"telegram_id", user.TelegramID,
		"days_until_expiry", daysUntilExpiry)

	return nil
}

// formatNotificationMessage formats the notification message
func (s *Service) formatNotificationMessage(sub *subs.Subscription, tariff *tariffs.Tariff, daysUntilExpiry int) string {
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

// runExpirationWorker marks expired subscriptions
func (s *Service) runExpirationWorker(ctx context.Context) error {
	s.logger.Info("Starting expiration worker execution")

	subscriptions, err := s.storage.ListExpiredSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("list expired subscriptions: %w", err)
	}

	s.logger.Info("Found expired subscriptions", "count", len(subscriptions))

	expiredStatus := subs.StatusExpired
	for _, sub := range subscriptions {
		criteria := subs.GetCriteria{IDs: []int64{sub.ID}}
		params := subs.UpdateParams{Status: &expiredStatus}

		_, err := s.storage.UpdateSubscription(ctx, criteria, params)
		if err != nil {
			s.logger.Error("Failed to expire subscription",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}

		s.logger.Info("Subscription expired",
			"subscription_id", sub.ID,
			"user_id", sub.UserID)
	}

	s.logger.Info("Expiration worker execution completed")
	return nil
}
