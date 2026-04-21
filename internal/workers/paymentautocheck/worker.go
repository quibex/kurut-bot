package paymentautocheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"kurut-bot/internal/infra/yookassa"
	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/webtokens"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

// Worker handles automatic payment status checking
type Worker struct {
	orderStorage        OrderStorage
	messageStorage      MessageStorage
	purchaseStorage     PurchaseTokenStorage
	renewalStorage      RenewalTokenStorage
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
	processingTokens   sync.Map
}

// NewWorker creates a new payment autocheck worker
func NewWorker(
	orderStorage OrderStorage,
	messageStorage MessageStorage,
	purchaseStorage PurchaseTokenStorage,
	renewalStorage RenewalTokenStorage,
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
		purchaseStorage:     purchaseStorage,
		renewalStorage:      renewalStorage,
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

// logProcessingErr logs a non-transient processing error. Callers must filter
// transient YooKassa errors (rate-limit / unavailable) upstream and aggregate
// those — we only emit ERROR here so the alerting rule on level="ERROR" stays
// signal.
func (w *Worker) logProcessingErr(msg string, err error, attrs ...any) {
	attrs = append(attrs, "error", err)
	w.logger.Error(msg, attrs...)
}

// logBatchTransient emits a single aggregated WARN line for transient
// YooKassa failures in a batch, instead of N per-item errors.
func (w *Worker) logBatchTransient(batch string, rateLimited, unavailable int32) {
	if rateLimited == 0 && unavailable == 0 {
		return
	}
	w.logger.Warn("YooKassa transient failures in batch (will retry next tick)",
		"batch", batch,
		"rate_limited", rateLimited,
		"unavailable", unavailable)
}

// Start starts the payment autocheck worker
func (w *Worker) Start() error {
	// Skip auto-check if manual payment mode is enabled
	if w.manualPayment {
		w.logger.Info("Manual payment mode enabled, skipping payment auto-check worker")
		return nil
	}

	// Run every 30 seconds. Previously 5s — YooKassa rate-limited us
	// under load and floated errors up the stack. Webhooks remain the
	// primary signal; this is a safety net.
	const interval = "@every 30s"
	_, err := w.cron.AddFunc(interval, func() {
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
	w.logger.Info("Payment autocheck worker started", "interval", interval)
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

	// Process purchase tokens (web purchases)
	if err := w.processPurchaseTokens(ctx); err != nil {
		w.logger.Error("Failed to process purchase tokens", "error", err)
	}

	return nil
}

// processPendingOrders handles pending orders with payments
func (w *Worker) processPendingOrders(ctx context.Context) error {
	pendingOrders, err := w.orderStorage.ListPendingOrdersWithPayments(ctx)
	if err != nil {
		return fmt.Errorf("list pending orders: %w", err)
	}

	var wg sync.WaitGroup
	var rateLimited, unavailable atomic.Int32

	for _, order := range pendingOrders {
		// Check if already being processed
		if _, loaded := w.processingOrders.LoadOrStore(order.ID, true); loaded {
			continue
		}

		wg.Add(1)
		go func(order *orders.PendingOrder) {
			defer wg.Done()
			defer w.processingOrders.Delete(order.ID)

			if err := w.processOrder(ctx, order); err != nil {
				if errors.Is(err, yookassa.ErrRateLimited) {
					rateLimited.Add(1)
					return
				}
				if errors.Is(err, yookassa.ErrUnavailable) {
					unavailable.Add(1)
					return
				}
				w.logProcessingErr("Failed to process order", err,
					"order_id", order.ID,
					"payment_id", order.PaymentID)
			}
		}(order)
	}

	wg.Wait()
	w.logBatchTransient("orders", rateLimited.Load(), unavailable.Load())

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
		// Still pending, will check again on the next tick
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
			ServerID:               order.ServerID,
			PaymentID:              &order.PaymentID,
			ClientWhatsApp:         order.ClientWhatsApp,
			CreatedByTelegramID:    order.AssistantTelegramID,
			ReferrerSubscriptionID: order.ReferrerSubscriptionID,
			ReferrerWhatsApp:       order.ReferrerWhatsApp,
			ReferralType:           order.ReferralType,
		}
		result, err = w.subscriptionService.CreateSubscription(ctx, req)
	}

	if err != nil {
		w.logger.Error("Failed to create subscription for order",
			"order_id", order.ID,
			"error", err)
		return fmt.Errorf("create subscription: %w", err)
	}

	// Generate renewal token for the subscription
	_, err = w.renewalStorage.GetOrCreateRenewalToken(ctx, result.Subscription.ID)
	if err != nil {
		w.logger.Warn("Failed to create renewal token",
			"subscription_id", result.Subscription.ID,
			"error", err)
		// Don't fail - just log
	}

	// Update Telegram message to show success
	if err := w.sendOrderSuccessMessage(order, result); err != nil {
		w.logger.Warn("Failed to send order success message",
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

	// Format WhatsApp link for web orders
	whatsappDisplay := order.ClientWhatsApp
	isWebOrder := order.ChatID == 0
	if isWebOrder {
		cleanNumber := strings.ReplaceAll(strings.ReplaceAll(order.ClientWhatsApp, "+", ""), " ", "")
		whatsappDisplay = fmt.Sprintf("[%s](https://wa.me/%s)", order.ClientWhatsApp, cleanNumber)
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
			whatsappDisplay, serverName, order.TariffName,
			result.GeneratedUserID, serverPassword)
	} else {
		if isWebOrder {
			text = fmt.Sprintf(
				"✅ *Новый клиент оплатил через сайт!*\n\n"+
					"📱 *WhatsApp:* %s\n"+
					"💰 *Тариф:* %s\n"+
					"🖥 *Сервер:* %s\n"+
					"🆔 *User ID:* `%s`\n"+
					"🔑 *Пароль сервера:* `%s`\n\n"+
					"⚡ *Требуется создать ключ на сервере*",
				whatsappDisplay, order.TariffName, serverName,
				result.GeneratedUserID, serverPassword)
		} else {
			text = fmt.Sprintf(
				"*Подписка создана*\n\n"+
					"*Клиент:* %s\n"+
					"*Тариф:* %s\n"+
					"*User ID:* `%s`\n"+
					"*Пароль:* `%s`",
				whatsappDisplay, order.TariffName,
				result.GeneratedUserID, serverPassword)
		}
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
	if order.MessageID != nil && order.ChatID != 0 {
		editMsg := tgbotapi.NewEditMessageText(order.ChatID, *order.MessageID, text)
		editMsg.ParseMode = "Markdown"
		editMsg.DisableWebPagePreview = true
		editMsg.ReplyMarkup = keyboard
		_, err := w.telegramBot.Send(editMsg)
		return err
	}

	// For web orders (ChatID=0), send to assistant
	targetChatID := order.ChatID
	if targetChatID == 0 {
		targetChatID = order.AssistantTelegramID
	}

	// Fallback: send new message
	msg := tgbotapi.NewMessage(targetChatID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
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

	var wg sync.WaitGroup
	var rateLimited, unavailable atomic.Int32

	for _, msg := range messages {
		// Check if already being processed
		if _, loaded := w.processingMessages.LoadOrStore(msg.ID, true); loaded {
			continue
		}

		wg.Add(1)
		go func(msg *submessages.SubscriptionMessage) {
			defer wg.Done()
			defer w.processingMessages.Delete(msg.ID)

			if err := w.processSubscriptionMessage(ctx, msg); err != nil {
				if errors.Is(err, yookassa.ErrRateLimited) {
					rateLimited.Add(1)
					return
				}
				if errors.Is(err, yookassa.ErrUnavailable) {
					unavailable.Add(1)
					return
				}
				w.logProcessingErr("Failed to process subscription message", err,
					"msg_id", msg.ID,
					"subscription_id", msg.SubscriptionID)
			}
		}(msg)
	}

	wg.Wait()
	w.logBatchTransient("subscription_messages", rateLimited.Load(), unavailable.Load())

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

	// Update Telegram message only if subscription was disabled (needs assistant action)
	// For active subscriptions, no notification needed - it's already renewed
	if wasDisabled {
		if err := w.sendRenewalSuccessMessage(msg, sub, tariff, server, wasDisabled); err != nil {
			w.logger.Warn("Failed to send renewal success message",
				"msg_id", msg.ID,
				"error", err)
		}
	} else {
		w.logger.Info("Skipping notification for active subscription renewal",
			"subscription_id", msg.SubscriptionID)
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
	whatsappLink := ""
	if sub.ClientWhatsApp != nil {
		whatsapp = *sub.ClientWhatsApp
		// Format WhatsApp link
		cleanNumber := strings.ReplaceAll(strings.ReplaceAll(*sub.ClientWhatsApp, "+", ""), " ", "")
		whatsappLink = fmt.Sprintf("https://wa.me/%s", cleanNumber)
	}

	// Add password line only if subscription was disabled
	passwordLine := ""
	if wasDisabled && server != nil && server.UIPassword != "" {
		passwordLine = fmt.Sprintf("\n🔑 *Пароль:* `%s`", server.UIPassword)
	}

	text := fmt.Sprintf(
		"🔄 *Продление отключенной подписки*\n\n"+
			"📱 *Клиент:* [%s](%s)\n"+
			"💰 *Тариф:* %s\n"+
			"📅 *Продлено на:* %d дней%s\n\n"+
			"⚡ *Требуется включить подписку на сервере*",
		whatsapp, whatsappLink, tariff.Name, tariff.DurationDays, passwordLine)

	// Build keyboard with server link and "Включил" button
	var rows [][]tgbotapi.InlineKeyboardButton
	if server != nil && server.UIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🌐 Сервер", server.UIURL),
		))
	}
	// Add "Включил" button with callback data
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✅ Включил", fmt.Sprintf("sub_enabled:%d", sub.ID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	// For web renewals (ChatID=0), send new message to the creator
	if msg.ChatID == 0 && sub.CreatedByTelegramID != nil {
		newMsg := tgbotapi.NewMessage(*sub.CreatedByTelegramID, text)
		newMsg.ParseMode = "Markdown"
		newMsg.ReplyMarkup = keyboard
		_, err := w.telegramBot.Send(newMsg)
		return err
	}

	editMsg := tgbotapi.NewEditMessageText(msg.ChatID, msg.MessageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &keyboard
	_, err := w.telegramBot.Send(editMsg)
	return err
}

// processPurchaseTokens handles purchase tokens with payments (web purchases)
func (w *Worker) processPurchaseTokens(ctx context.Context) error {
	tokens, err := w.purchaseStorage.ListPaidPurchaseTokens(ctx)
	if err != nil {
		return fmt.Errorf("list paid purchase tokens: %w", err)
	}

	var wg sync.WaitGroup
	var rateLimited, unavailable atomic.Int32

	for _, token := range tokens {
		// Check if already being processed
		if _, loaded := w.processingTokens.LoadOrStore(token.ID, true); loaded {
			continue
		}

		wg.Add(1)
		go func(token *webtokens.PurchaseToken) {
			defer wg.Done()
			defer w.processingTokens.Delete(token.ID)

			if err := w.processPurchaseToken(ctx, token); err != nil {
				if errors.Is(err, yookassa.ErrRateLimited) {
					rateLimited.Add(1)
					return
				}
				if errors.Is(err, yookassa.ErrUnavailable) {
					unavailable.Add(1)
					return
				}
				w.logProcessingErr("Failed to process purchase token", err,
					"token_id", token.ID,
					"payment_id", token.PaymentID)
			}
		}(token)
	}

	wg.Wait()
	w.logBatchTransient("purchase_tokens", rateLimited.Load(), unavailable.Load())

	return nil
}

// processPurchaseToken processes a single purchase token
func (w *Worker) processPurchaseToken(ctx context.Context, token *webtokens.PurchaseToken) error {
	if token.PaymentID == nil {
		return nil
	}

	// Check payment status
	paymentObj, err := w.paymentService.CheckPaymentStatus(ctx, *token.PaymentID)
	if err != nil {
		return fmt.Errorf("check payment status: %w", err)
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return w.handleApprovedPurchasePayment(ctx, token)
	case payment.StatusRejected, payment.StatusCancelled:
		w.logger.Info("Purchase payment rejected/cancelled",
			"token_id", token.ID,
			"payment_id", *token.PaymentID,
			"status", paymentObj.Status)
		// Update status to cancelled
		_ = w.purchaseStorage.UpdatePurchaseTokenStatus(ctx, token.ID, webtokens.PurchaseStatusCancelled)
		return nil
	case payment.StatusPending:
		// Still pending, will check again
		return nil
	default:
		return nil
	}
}

// handleApprovedPurchasePayment handles a successful payment for purchase token
func (w *Worker) handleApprovedPurchasePayment(ctx context.Context, token *webtokens.PurchaseToken) error {
	w.logger.Info("Processing approved payment for purchase token",
		"token_id", token.ID,
		"payment_id", *token.PaymentID)

	if token.TariffID == nil {
		return fmt.Errorf("purchase token has no tariff_id")
	}

	// Find referrer subscription ID if referrer WhatsApp is provided
	var referrerSubID *int64
	if token.ReferrerWhatsApp != nil && *token.ReferrerWhatsApp != "" {
		// Find active subscription by WhatsApp
		sub, err := w.subscriptionStorage.GetSubscription(ctx, subs.GetCriteria{})
		if err == nil && sub != nil && sub.ClientWhatsApp != nil && *sub.ClientWhatsApp == *token.ReferrerWhatsApp {
			referrerSubID = &sub.ID
		}
	}

	// Create subscription
	req := &subs.CreateSubscriptionRequest{
		UserID:                 token.CreatedByTelegramID,
		TariffID:               *token.TariffID,
		PaymentID:              token.PaymentID,
		ClientWhatsApp:         token.ClientWhatsApp,
		CreatedByTelegramID:    token.CreatedByTelegramID,
		ReferrerSubscriptionID: referrerSubID,
	}

	result, err := w.subscriptionService.CreateSubscription(ctx, req)
	if err != nil {
		w.logger.Error("Failed to create subscription for purchase token",
			"token_id", token.ID,
			"error", err)
		return fmt.Errorf("create subscription: %w", err)
	}

	// Generate renewal token for the subscription
	_, err = w.renewalStorage.GetOrCreateRenewalToken(ctx, result.Subscription.ID)
	if err != nil {
		w.logger.Warn("Failed to create renewal token",
			"subscription_id", result.Subscription.ID,
			"error", err)
		// Don't fail - just log
	}

	// Send notification to admin
	if err := w.sendPurchaseSuccessMessage(token, result); err != nil {
		w.logger.Warn("Failed to send purchase success message",
			"token_id", token.ID,
			"error", err)
	}

	// Update token status to completed
	if err := w.purchaseStorage.UpdatePurchaseTokenStatus(ctx, token.ID, webtokens.PurchaseStatusCompleted); err != nil {
		w.logger.Error("Failed to update purchase token status",
			"token_id", token.ID,
			"error", err)
	}

	w.logger.Info("Successfully processed purchase token payment",
		"token_id", token.ID,
		"subscription_id", result.Subscription.ID)

	return nil
}

// sendPurchaseSuccessMessage sends notification to admin about successful purchase
func (w *Worker) sendPurchaseSuccessMessage(token *webtokens.PurchaseToken, result *subs.CreateSubscriptionResult) error {
	serverURL := ""
	serverPassword := ""
	if result.ServerUIURL != nil {
		serverURL = *result.ServerUIURL
	}
	if result.ServerUIPassword != nil {
		serverPassword = *result.ServerUIPassword
	}

	// Get tariff name
	tariffName := "Неизвестно"
	if token.TariffID != nil {
		tariff, err := w.tariffService.GetTariff(context.Background(), tariffs.GetCriteria{ID: token.TariffID})
		if err == nil && tariff != nil {
			tariffName = tariff.Name
		}
	}

	// Format WhatsApp link
	whatsappLink := ""
	cleanNumber := strings.ReplaceAll(strings.ReplaceAll(token.ClientWhatsApp, "+", ""), " ", "")
	whatsappLink = fmt.Sprintf("https://wa.me/%s", cleanNumber)

	text := fmt.Sprintf(
		"✅ *Новый клиент оплатил через сайт!*\n\n"+
			"📱 *WhatsApp:* [%s](%s)\n"+
			"💰 *Тариф:* %s\n"+
			"🆔 *User ID:* `%s`\n"+
			"🔑 *Пароль сервера:* `%s`\n\n"+
			"⚡ *Требуется создать ключ на сервере*",
		token.ClientWhatsApp, whatsappLink, tariffName,
		result.GeneratedUserID, serverPassword)

	// Add referral bonus info if applicable
	if result.ReferralBonusApplied && result.ReferrerWhatsApp != nil {
		text += fmt.Sprintf("\n\n👤 *Пригласил:* %s (+10 дней бонус)", *result.ReferrerWhatsApp)
	}

	// Build keyboard with server link
	var rows [][]tgbotapi.InlineKeyboardButton
	if serverURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🌐 Сервер", serverURL),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	// Send to admin who created the token
	msg := tgbotapi.NewMessage(token.CreatedByTelegramID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err := w.telegramBot.Send(msg)
	return err
}
