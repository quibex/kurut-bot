package paymentautocheck

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

// Worker handles automatic payment status checking
type Worker struct {
	orderStorage        OrderStorage
	messageStorage      MessageStorage
	paymentService      PaymentService
	subscriptionService SubscriptionService
	subscriptionStorage SubscriptionStorage
	tariffService       TariffService
	serverStorage       ServerStorage
	telegramBot         TelegramBot
	logger              *slog.Logger
	cron                *cron.Cron
	manualPayment       bool

	// Track orders being processed to prevent race conditions
	processingOrders   sync.Map
	processingMessages sync.Map
}

// NewWorker creates a new payment autocheck worker
func NewWorker(
	orderStorage OrderStorage,
	messageStorage MessageStorage,
	paymentService PaymentService,
	subscriptionService SubscriptionService,
	subscriptionStorage SubscriptionStorage,
	tariffService TariffService,
	serverStorage ServerStorage,
	telegramBot TelegramBot,
	manualPayment bool,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		orderStorage:        orderStorage,
		messageStorage:      messageStorage,
		paymentService:      paymentService,
		subscriptionService: subscriptionService,
		subscriptionStorage: subscriptionStorage,
		tariffService:       tariffService,
		serverStorage:       serverStorage,
		telegramBot:         telegramBot,
		logger:              logger,
		cron:                cron.New(),
		manualPayment:       manualPayment,
	}
}

// Name returns the worker name
func (w *Worker) Name() string {
	return "payment-autocheck"
}

// Start starts the payment autocheck worker
func (w *Worker) Start() error {
	// Skip auto-check if manual payment mode is enabled
	if w.manualPayment {
		w.logger.Info("Manual payment mode enabled, skipping payment auto-check worker")
		return nil
	}

	// Run every 5 seconds
	_, err := w.cron.AddFunc("@every 5s", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in payment autocheck worker", "panic", r)
			}
		}()
		ctx := context.Background()
		if err := w.run(ctx); err != nil {
			w.logger.Error("Payment autocheck worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule payment autocheck worker: %w", err)
	}

	w.cron.Start()
	w.logger.Info("Payment autocheck worker started", "interval", "5s")
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping payment autocheck worker")
	w.cron.Stop()
}

// run executes the payment check logic
func (w *Worker) run(ctx context.Context) error {
	// Process pending orders (new subscriptions and migrations)
	if err := w.processPendingOrders(ctx); err != nil {
		w.logger.Error("Failed to process pending orders", "error", err)
	}

	// Process subscription messages (extensions/renewals)
	if err := w.processSubscriptionMessages(ctx); err != nil {
		w.logger.Error("Failed to process subscription messages", "error", err)
	}

	return nil
}

// processPendingOrders handles pending orders with payments
func (w *Worker) processPendingOrders(ctx context.Context) error {
	pendingOrders, err := w.orderStorage.ListPendingOrdersWithPayments(ctx)
	if err != nil {
		return fmt.Errorf("list pending orders: %w", err)
	}

	for _, order := range pendingOrders {
		// Check if already being processed
		if _, loaded := w.processingOrders.LoadOrStore(order.ID, true); loaded {
			continue
		}

		// Process in goroutine to not block other orders
		go func(order *orders.PendingOrder) {
			defer w.processingOrders.Delete(order.ID)

			if err := w.processOrder(ctx, order); err != nil {
				w.logger.Error("Failed to process order",
					"order_id", order.ID,
					"payment_id", order.PaymentID,
					"error", err)
			}
		}(order)
	}

	return nil
}

// processOrder processes a single pending order
func (w *Worker) processOrder(ctx context.Context, order *orders.PendingOrder) error {
	// Check payment status
	paymentObj, err := w.paymentService.CheckPaymentStatus(ctx, order.PaymentID)
	if err != nil {
		return fmt.Errorf("check payment status: %w", err)
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return w.handleApprovedOrderPayment(ctx, order)
	case payment.StatusRejected, payment.StatusCancelled:
		w.logger.Info("Order payment rejected/cancelled",
			"order_id", order.ID,
			"payment_id", order.PaymentID,
			"status", paymentObj.Status)
		// Don't delete - user can refresh the payment link
		return nil
	case payment.StatusPending:
		// Still pending, will check again in 5 seconds
		return nil
	default:
		return nil
	}
}

// handleApprovedOrderPayment handles a successful payment for an order
func (w *Worker) handleApprovedOrderPayment(ctx context.Context, order *orders.PendingOrder) error {
	w.logger.Info("Processing approved payment for order",
		"order_id", order.ID,
		"payment_id", order.PaymentID,
		"is_migration", order.IsMigration())

	var result *subs.CreateSubscriptionResult
	var err error

	if order.IsMigration() {
		// Migration order - use MigrateSubscription
		req := &subs.MigrateSubscriptionRequest{
			UserID:              order.AdminUserID,
			TariffID:            order.TariffID,
			ServerID:            *order.ServerID,
			ClientWhatsApp:      order.ClientWhatsApp,
			CreatedByTelegramID: order.AssistantTelegramID,
		}
		result, err = w.subscriptionService.MigrateSubscription(ctx, req)
	} else {
		// New subscription order - use CreateSubscription
		req := &subs.CreateSubscriptionRequest{
			UserID:                 order.AdminUserID,
			TariffID:               order.TariffID,
			PaymentID:              &order.PaymentID,
			ClientWhatsApp:         order.ClientWhatsApp,
			CreatedByTelegramID:    order.AssistantTelegramID,
			ReferrerSubscriptionID: order.ReferrerSubscriptionID,
		}
		result, err = w.subscriptionService.CreateSubscription(ctx, req)
	}

	if err != nil {
		w.logger.Error("Failed to create subscription for order",
			"order_id", order.ID,
			"error", err)
		return fmt.Errorf("create subscription: %w", err)
	}

	// Update Telegram message to show success
	if err := w.sendOrderSuccessMessage(order, result); err != nil {
		w.logger.Error("Failed to send order success message",
			"order_id", order.ID,
			"error", err)
	}

	// Delete the pending order
	if err := w.orderStorage.DeletePendingOrder(ctx, order.ID); err != nil {
		w.logger.Error("Failed to delete pending order",
			"order_id", order.ID,
			"error", err)
	}

	w.logger.Info("Successfully processed order payment",
		"order_id", order.ID,
		"subscription_id", result.Subscription.ID)

	return nil
}

// sendOrderSuccessMessage sends/updates the Telegram message for a successful order
func (w *Worker) sendOrderSuccessMessage(order *orders.PendingOrder, result *subs.CreateSubscriptionResult) error {
	serverURL := ""
	serverPassword := ""
	if result.ServerUIURL != nil {
		serverURL = *result.ServerUIURL
	}
	if result.ServerUIPassword != nil {
		serverPassword = *result.ServerUIPassword
	}

	serverName := ""
	if order.ServerName != nil {
		serverName = *order.ServerName
	}

	var text string
	if order.IsMigration() {
		text = fmt.Sprintf(
			"*Подписка создана (миграция)*\n\n"+
				"*Клиент:* %s\n"+
				"*Сервер:* %s\n"+
				"*Тариф:* %s\n"+
				"*User ID:* `%s`\n"+
				"*Пароль:* `%s`",
			order.ClientWhatsApp, serverName, order.TariffName,
			result.GeneratedUserID, serverPassword)
	} else {
		text = fmt.Sprintf(
			"*Подписка создана*\n\n"+
				"*Клиент:* %s\n"+
				"*Тариф:* %s\n"+
				"*User ID:* `%s`\n"+
				"*Пароль:* `%s`",
			order.ClientWhatsApp, order.TariffName,
			result.GeneratedUserID, serverPassword)
	}

	// Add referral bonus info if applicable
	if result.ReferralBonusApplied && result.ReferrerWhatsApp != nil {
		text += fmt.Sprintf("\n\n*Реферальный бонус*: +10 дней для %s", *result.ReferrerWhatsApp)
	}

	// Build keyboard with server link
	var rows [][]tgbotapi.InlineKeyboardButton
	if serverURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Сервер", serverURL),
		))
	}

	var keyboard *tgbotapi.InlineKeyboardMarkup
	if len(rows) > 0 {
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		keyboard = &kb
	}

	// Edit existing message or send new one
	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(order.ChatID, *order.MessageID, text)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = keyboard
		_, err := w.telegramBot.Send(editMsg)
		return err
	}

	// Fallback: send new message
	msg := tgbotapi.NewMessage(order.ChatID, text)
	msg.ParseMode = "Markdown"
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	_, err := w.telegramBot.Send(msg)
	return err
}

// processSubscriptionMessages handles subscription messages with payments (renewals)
func (w *Worker) processSubscriptionMessages(ctx context.Context) error {
	messages, err := w.messageStorage.ListActiveMessagesWithPayments(ctx)
	if err != nil {
		return fmt.Errorf("list active messages: %w", err)
	}

	for _, msg := range messages {
		// Check if already being processed
		if _, loaded := w.processingMessages.LoadOrStore(msg.ID, true); loaded {
			continue
		}

		// Process in goroutine
		go func(msg *submessages.SubscriptionMessage) {
			defer w.processingMessages.Delete(msg.ID)

			if err := w.processSubscriptionMessage(ctx, msg); err != nil {
				w.logger.Error("Failed to process subscription message",
					"msg_id", msg.ID,
					"subscription_id", msg.SubscriptionID,
					"error", err)
			}
		}(msg)
	}

	return nil
}

// processSubscriptionMessage processes a single subscription message
func (w *Worker) processSubscriptionMessage(ctx context.Context, msg *submessages.SubscriptionMessage) error {
	if msg.PaymentID == nil {
		return nil
	}

	// Check payment status
	paymentObj, err := w.paymentService.CheckPaymentStatus(ctx, *msg.PaymentID)
	if err != nil {
		return fmt.Errorf("check payment status: %w", err)
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return w.handleApprovedRenewalPayment(ctx, msg)
	case payment.StatusRejected, payment.StatusCancelled:
		w.logger.Info("Renewal payment rejected/cancelled",
			"msg_id", msg.ID,
			"payment_id", *msg.PaymentID,
			"status", paymentObj.Status)
		// Don't deactivate - user can create new payment link
		return nil
	case payment.StatusPending:
		// Still pending, will check again
		return nil
	default:
		return nil
	}
}

// handleApprovedRenewalPayment handles a successful payment for subscription renewal
func (w *Worker) handleApprovedRenewalPayment(ctx context.Context, msg *submessages.SubscriptionMessage) error {
	w.logger.Info("Processing approved renewal payment",
		"msg_id", msg.ID,
		"subscription_id", msg.SubscriptionID,
		"payment_id", *msg.PaymentID)

	// Get the subscription
	sub, err := w.subscriptionStorage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{msg.SubscriptionID}})
	if err != nil || sub == nil {
		return fmt.Errorf("get subscription: %w", err)
	}

	// Determine tariff to use
	tariffID := sub.TariffID
	if msg.SelectedTariffID != nil {
		tariffID = *msg.SelectedTariffID
		// Update subscription tariff if changed
		if tariffID != sub.TariffID {
			if err := w.subscriptionStorage.UpdateSubscriptionTariff(ctx, msg.SubscriptionID, tariffID); err != nil {
				w.logger.Error("Failed to update subscription tariff",
					"subscription_id", msg.SubscriptionID,
					"tariff_id", tariffID,
					"error", err)
			}
		}
	}

	// Get tariff for duration
	tariff, err := w.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &tariffID})
	if err != nil || tariff == nil {
		return fmt.Errorf("get tariff: %w", err)
	}

	// Extend subscription
	if err := w.subscriptionStorage.ExtendSubscription(ctx, msg.SubscriptionID, tariff.DurationDays); err != nil {
		return fmt.Errorf("extend subscription: %w", err)
	}

	// Set status to active (if was expired/disabled)
	wasDisabled := sub.Status == subs.StatusDisabled
	activeStatus := subs.StatusActive
	_, err = w.subscriptionStorage.UpdateSubscription(ctx, subs.GetCriteria{IDs: []int64{msg.SubscriptionID}}, subs.UpdateParams{
		Status: &activeStatus,
	})
	if err != nil {
		w.logger.Error("Failed to update subscription status",
			"subscription_id", msg.SubscriptionID,
			"error", err)
	}

	// Get server info for message
	var server *servers.Server
	if sub.ServerID != nil {
		server, _ = w.serverStorage.GetServer(ctx, servers.GetCriteria{ID: sub.ServerID})
	}

	// Update Telegram message
	if err := w.sendRenewalSuccessMessage(msg, sub, tariff, server, wasDisabled); err != nil {
		w.logger.Error("Failed to send renewal success message",
			"msg_id", msg.ID,
			"error", err)
	}

	// Deactivate the subscription message
	if err := w.messageStorage.DeactivateSubscriptionMessage(ctx, msg.ID); err != nil {
		w.logger.Error("Failed to deactivate subscription message",
			"msg_id", msg.ID,
			"error", err)
	}

	w.logger.Info("Successfully processed renewal payment",
		"msg_id", msg.ID,
		"subscription_id", msg.SubscriptionID,
		"days_added", tariff.DurationDays)

	return nil
}

// sendRenewalSuccessMessage updates the Telegram message after successful renewal
func (w *Worker) sendRenewalSuccessMessage(
	msg *submessages.SubscriptionMessage,
	sub *subs.Subscription,
	tariff *tariffs.Tariff,
	server *servers.Server,
	wasDisabled bool,
) error {
	whatsapp := "Не указан"
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
	}

	// Add password line only if subscription was disabled
	passwordLine := ""
	if wasDisabled && server != nil && server.UIPassword != "" {
		passwordLine = fmt.Sprintf("\n*Пароль:* `%s`", server.UIPassword)
	}

	text := fmt.Sprintf(
		"*Подписка продлена*\n\n"+
			"*Клиент:* %s\n"+
			"*Тариф:* %s\n"+
			"*Продлено на:* %d дней%s",
		whatsapp, tariff.Name, tariff.DurationDays, passwordLine)

	// Build keyboard with server link if subscription was disabled
	var rows [][]tgbotapi.InlineKeyboardButton
	if wasDisabled && server != nil && server.UIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Сервер", server.UIURL),
		))
	}

	var keyboard *tgbotapi.InlineKeyboardMarkup
	if len(rows) > 0 {
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		keyboard = &kb
	}

	editMsg := tgbotapi.NewEditMessageText(msg.ChatID, msg.MessageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = keyboard
	_, err := w.telegramBot.Send(editMsg)
	return err
}
