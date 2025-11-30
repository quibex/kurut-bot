package retrysubscription

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"
	"kurut-bot/internal/telegram/messages"

	"github.com/robfig/cron/v3"
)

// Worker handles retrying subscription creation for orphaned payments
type Worker struct {
	storage             Storage
	subscriptionService SubscriptionService
	telegramBot         TelegramBot
	logger              *slog.Logger
	cron                *cron.Cron
}

// NewWorker creates a new retry subscription worker
func NewWorker(
	storage Storage,
	subscriptionService SubscriptionService,
	telegramBot TelegramBot,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		storage:             storage,
		subscriptionService: subscriptionService,
		telegramBot:         telegramBot,
		logger:              logger,
		cron:                cron.New(),
	}
}

// Name returns the worker name
func (w *Worker) Name() string {
	return "retry-subscription"
}

// Start starts the retry subscription worker
func (w *Worker) Start() error {
	// Runs every 5 minutes
	_, err := w.cron.AddFunc("*/5 * * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in retry subscription worker", "panic", r)
			}
		}()
		ctx := context.Background()
		w.logger.Info("Running retry subscription worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Retry subscription worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule retry subscription worker: %w", err)
	}

	w.cron.Start()
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping retry subscription worker")
	w.cron.Stop()
}

// run executes the retry subscription logic
func (w *Worker) run(ctx context.Context) error {
	w.logger.Info("Starting retry subscription worker execution")

	orphanedPayments, err := w.storage.ListOrphanedPayments(ctx)
	if err != nil {
		return fmt.Errorf("list orphaned payments: %w", err)
	}

	if len(orphanedPayments) == 0 {
		return nil
	}

	w.logger.Info("Found orphaned payments", "count", len(orphanedPayments))

	for _, payment := range orphanedPayments {
		if err := w.processOrphanedPayment(ctx, payment); err != nil {
			w.logger.Error("Failed to process orphaned payment",
				"payment_id", payment.ID,
				"user_id", payment.UserID,
				"error", err)
			continue
		}
	}

	w.logger.Info("Retry subscription worker execution completed")
	return nil
}

// processOrphanedPayment processes a single orphaned payment
func (w *Worker) processOrphanedPayment(ctx context.Context, payment *payment.Payment) error {
	w.logger.Info("Processing orphaned payment",
		"payment_id", payment.ID,
		"user_id", payment.UserID,
		"amount", payment.Amount)

	// Get user information
	user, err := w.storage.GetUser(ctx, users.GetCriteria{ID: &payment.UserID})
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found: %d", payment.UserID)
	}

	// Find tariff by price
	tariff, err := w.findTariffByPrice(ctx, payment.Amount)
	if err != nil {
		return fmt.Errorf("find tariff by price: %w", err)
	}
	if tariff == nil {
		return fmt.Errorf("no tariff found matching payment amount: %.2f", payment.Amount)
	}

	// Check if there's already a subscription for this user+tariff without payment link
	// This can happen if subscription was created but payment linking failed
	// Search for ANY status subscription, not just Active
	existingSubs, err := w.storage.ListSubscriptions(ctx, subs.ListCriteria{
		UserIDs:   []int64{user.ID},
		TariffIDs: []int64{tariff.ID},
		// Don't filter by status - check all subscriptions
		Limit: 10,
	})
	if err != nil {
		return fmt.Errorf("check existing subscriptions: %w", err)
	}

	w.logger.Info("Found existing subscriptions for user+tariff",
		"payment_id", payment.ID,
		"user_id", user.ID,
		"tariff_id", tariff.ID,
		"count", len(existingSubs))

	var subscription *subs.Subscription

	// Try to find a subscription without payment link
	for _, sub := range existingSubs {
		w.logger.Info("Checking subscription",
			"subscription_id", sub.ID,
			"status", sub.Status)
		// Check if this subscription is already linked to ANY payment
		linkedToPayment, err := w.storage.IsSubscriptionLinkedToPayment(ctx, sub.ID)
		if err != nil {
			w.logger.Warn("Failed to check if subscription is linked to payment", "error", err)
			continue
		}

		w.logger.Info("Subscription payment link status",
			"subscription_id", sub.ID,
			"linked_to_payment", linkedToPayment)

		if !linkedToPayment {
			// Found a subscription without payment - use it
			subscription = sub
			w.logger.Info("Found existing subscription without payment link",
				"subscription_id", sub.ID,
				"payment_id", payment.ID)
			break
		}
	}

	// If no existing subscription found, create a new one
	if subscription == nil {
		req := &subs.CreateSubscriptionRequest{
			UserID:    user.ID,
			TariffID:  tariff.ID,
			PaymentID: &payment.ID,
		}

		subscription, err = w.subscriptionService.CreateSubscription(ctx, req)
		if err != nil {
			return fmt.Errorf("create subscription: %w", err)
		}

		w.logger.Info("Successfully created new subscription for orphaned payment",
			"payment_id", payment.ID,
			"subscription_id", subscription.ID,
			"user_id", user.ID)
	} else {
		// Link the existing subscription to this payment
		if err := w.storage.LinkPaymentToSubscriptions(ctx, payment.ID, []int64{subscription.ID}); err != nil {
			return fmt.Errorf("link existing subscription to payment: %w", err)
		}

		w.logger.Info("Successfully linked existing subscription to orphaned payment",
			"payment_id", payment.ID,
			"subscription_id", subscription.ID,
			"user_id", user.ID)
	}

	// Send notification to user
	if err := w.sendRetrySuccessNotification(ctx, user, subscription, tariff); err != nil {
		w.logger.Error("Failed to send notification to user",
			"user_id", user.ID,
			"telegram_id", user.TelegramID,
			"error", err)
	}

	return nil
}

// findTariffByPrice finds a tariff matching the payment amount
func (w *Worker) findTariffByPrice(ctx context.Context, price float64) (*tariffs.Tariff, error) {
	allTariffs, err := w.storage.ListTariffs(ctx, tariffs.ListCriteria{})
	if err != nil {
		return nil, fmt.Errorf("list tariffs: %w", err)
	}

	for _, tariff := range allTariffs {
		if tariff.Price == price {
			return tariff, nil
		}
	}

	return nil, nil
}

// sendRetrySuccessNotification sends notification to user
func (w *Worker) sendRetrySuccessNotification(ctx context.Context, user *users.User, subscription *subs.Subscription, tariff *tariffs.Tariff) error {
	message := messages.FormatSubscriptionRetrySuccess(tariff.Name)

	wgData, err := subscription.GetWireGuardData()
	if err == nil && wgData != nil && wgData.ConfigFile != "" {
		message += "\n```\n" + wgData.ConfigFile + "\n```"
	}

	message += "\n\n" + messages.SubscriptionRetrySuccessBody

	if err := w.telegramBot.SendMessage(user.TelegramID, message); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}

	w.logger.Info("Retry success notification sent",
		"user_id", user.ID,
		"telegram_id", user.TelegramID,
		"subscription_id", subscription.ID)

	return nil
}
