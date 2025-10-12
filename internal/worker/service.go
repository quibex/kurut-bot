package worker

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/payment"
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
	storage             Storage
	telegramBot         TelegramBot
	tariffStorage       tariffStorage
	subscriptionService SubscriptionService
	localizer           Localizer
	logger              *slog.Logger
	cron                *cron.Cron
}

type tariffStorage interface {
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
}

func NewService(storage Storage, telegramBot TelegramBot, tariffStorage tariffStorage, subscriptionService SubscriptionService, localizer Localizer, logger *slog.Logger) *Service {
	return &Service{
		storage:             storage,
		telegramBot:         telegramBot,
		tariffStorage:       tariffStorage,
		subscriptionService: subscriptionService,
		localizer:           localizer,
		logger:              logger,
		cron:                cron.New(),
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

	// Retry subscription worker: runs every 5 minutes
	_, err = s.cron.AddFunc("*/5 * * * *", func() {
		ctx := context.Background()
		s.logger.Info("Running retry subscription worker")
		if err := s.runRetrySubscriptionWorker(ctx); err != nil {
			s.logger.Error("Retry subscription worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add retry subscription worker: %w", err)
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

// runRetrySubscriptionWorker retries subscription creation for paid but failed orders
func (s *Service) runRetrySubscriptionWorker(ctx context.Context) error {
	s.logger.Info("Starting retry subscription worker execution")

	orphanedPayments, err := s.storage.ListOrphanedPayments(ctx)
	if err != nil {
		return fmt.Errorf("list orphaned payments: %w", err)
	}

	s.logger.Info("Found orphaned payments", "count", len(orphanedPayments))

	for _, payment := range orphanedPayments {
		if err := s.processOrphanedPayment(ctx, payment); err != nil {
			s.logger.Error("Failed to process orphaned payment",
				"payment_id", payment.ID,
				"user_id", payment.UserID,
				"error", err)
			continue
		}
	}

	s.logger.Info("Retry subscription worker execution completed")
	return nil
}

// processOrphanedPayment processes a single orphaned payment by creating subscription and notifying user
func (s *Service) processOrphanedPayment(ctx context.Context, payment *payment.Payment) error {
	s.logger.Info("Processing orphaned payment",
		"payment_id", payment.ID,
		"user_id", payment.UserID,
		"amount", payment.Amount)

	// Get user information
	user, err := s.storage.GetUser(ctx, users.GetCriteria{ID: &payment.UserID})
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found: %d", payment.UserID)
	}

	// Get payment subscriptions to find tariff (check if payment was linked to any prior subscription attempt)
	// Since we're looking for orphaned payments, we need to infer the tariff from the payment amount
	// For now, we'll need to get the tariff based on price matching
	tariff, err := s.findTariffByPrice(ctx, payment.Amount)
	if err != nil {
		return fmt.Errorf("find tariff by price: %w", err)
	}
	if tariff == nil {
		return fmt.Errorf("no tariff found matching payment amount: %.2f", payment.Amount)
	}

	// Create subscription request
	req := &subs.CreateSubscriptionRequest{
		UserID:    user.ID,
		TariffID:  tariff.ID,
		PaymentID: &payment.ID,
	}

	// Try to create subscription
	subscription, err := s.subscriptionService.CreateSubscription(ctx, req)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}

	s.logger.Info("Successfully created subscription for orphaned payment",
		"payment_id", payment.ID,
		"subscription_id", subscription.ID,
		"user_id", user.ID)

	// Send notification to user
	if err := s.sendRetrySuccessNotification(ctx, user, subscription, tariff); err != nil {
		s.logger.Error("Failed to send notification to user",
			"user_id", user.ID,
			"telegram_id", user.TelegramID,
			"error", err)
	}

	return nil
}

// findTariffByPrice attempts to find a tariff matching the payment amount
func (s *Service) findTariffByPrice(ctx context.Context, price float64) (*tariffs.Tariff, error) {
	// List all tariffs
	allTariffs, err := s.storage.ListTariffs(ctx, tariffs.ListCriteria{})
	if err != nil {
		return nil, fmt.Errorf("list tariffs: %w", err)
	}

	// Find tariff with matching price
	for _, tariff := range allTariffs {
		if tariff.Price == price {
			return tariff, nil
		}
	}

	return nil, nil
}

// sendRetrySuccessNotification sends a notification to user when subscription is successfully created after retry
func (s *Service) sendRetrySuccessNotification(ctx context.Context, user *users.User, subscription *subs.Subscription, tariff *tariffs.Tariff) error {
	lang := user.Language
	if lang == "" {
		lang = "ru"
	}

	message := s.localizer.Get(lang, "subscription.retry_success", map[string]interface{}{
		"tariff_name": tariff.Name,
	})

	if subscription.MarzbanLink != "" {
		message += "\n\n" + subscription.MarzbanLink
	}

	message += "\n\n" + s.localizer.Get(lang, "subscription.retry_success_body", nil)

	if err := s.telegramBot.SendMessage(user.TelegramID, message); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}

	s.logger.Info("Retry success notification sent",
		"user_id", user.ID,
		"telegram_id", user.TelegramID,
		"subscription_id", subscription.ID)

	return nil
}
