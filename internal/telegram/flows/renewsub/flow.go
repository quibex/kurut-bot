package renewsub

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot                 botApi
	stateManager        stateManager
	subscriptionService subscriptionService
	tariffService       tariffService
	paymentService      paymentService
	l10n                localizer
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ss subscriptionService,
	ts tariffService,
	ps paymentService,
	l10n localizer,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		subscriptionService: ss,
		tariffService:       ts,
		paymentService:      ps,
		l10n:                l10n,
		logger:              logger,
	}
}

// Start starts the renewal flow
func (h *Handler) Start(userID, chatID int64, lang string) error {
	flowData := &flows.RenewSubFlowData{
		UserID:   userID,
		Language: lang,
	}
	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	return h.showSubscriptions(chatID, userID, lang)
}

// Handle processes the current state
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.UserRenewSubWaitSelection:
		return h.handleSubscriptionSelection(ctx, update)
	case states.UserRenewSubWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.UserRenewSubWaitPayment:
		return h.handlePaymentConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown state: %s", state)
	}
}

// showSubscriptions shows user's active and expired subscriptions
func (h *Handler) showSubscriptions(chatID, userID int64, lang string) error {
	ctx := context.Background()

	subscriptions, err := h.subscriptionService.ListSubscriptions(ctx, subs.ListCriteria{
		UserIDs: []int64{userID},
		Status:  []subs.Status{subs.StatusActive, subs.StatusExpired},
	})
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "renew.no_subscriptions", nil))
		_, err = h.bot.Send(msg)
		return err
	}

	keyboard, err := h.createSubscriptionsKeyboard(ctx, subscriptions, lang)
	if err != nil {
		return fmt.Errorf("create subscriptions keyboard: %w", err)
	}

	msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "renew.choose_subscription", nil))
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

// createSubscriptionsKeyboard creates keyboard with subscriptions
func (h *Handler) createSubscriptionsKeyboard(ctx context.Context, subscriptions []*subs.Subscription, lang string) (tgbotapi.InlineKeyboardMarkup, error) {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, sub := range subscriptions {
		expiresText := "неизвестно"
		statusEmoji := "🔑"

		if sub.ExpiresAt != nil {
			expiresText = sub.ExpiresAt.Format("02.01.2006")
		}

		if sub.Status == subs.StatusExpired {
			statusEmoji = "🔴"
		}

		// Get tariff name
		tariff, err := h.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &sub.TariffID})
		if err != nil {
			h.logger.Warn("Failed to get tariff", "error", err, "tariff_id", sub.TariffID)
			continue
		}

		tariffName := tariff.Name
		if tariffName == "" {
			tariffName = fmt.Sprintf("Tariff #%d", sub.TariffID)
		}

		text := h.l10n.Get(lang, "renew.subscription_button", map[string]interface{}{
			"id":          sub.ID,
			"tariff_name": tariffName,
			"expires_at":  expiresText,
		})
		text = fmt.Sprintf("%s %s", statusEmoji, text)
		callbackData := fmt.Sprintf("renew_sub:%d", sub.ID)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(h.l10n.Get(lang, "buttons.cancel", nil), "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

// handleSubscriptionSelection handles subscription selection
func (h *Handler) handleSubscriptionSelection(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Получаем данные флоу для языка
	flowData, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, "ru", h.l10n.Get("ru", "flows.error_getting_data", nil))
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "flows.use_buttons", nil))
	}

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if !strings.HasPrefix(update.CallbackQuery.Data, "renew_sub:") {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "renew.invalid_subscription", nil))
	}

	parts := strings.Split(update.CallbackQuery.Data, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "renew.invalid_subscription", nil))
	}

	subID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "renew.invalid_subscription", nil))
	}

	flowData.SubscriptionID = subID

	// Получаем информацию о подписке, чтобы проверить, есть ли client_name
	subscription, err := h.subscriptionService.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "flows.error_getting_data", nil))
	}

	// Сохраняем client_name если он есть
	if subscription != nil && subscription.ClientName != nil {
		flowData.ClientName = subscription.ClientName
	}

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, h.l10n.Get(flowData.Language, "payment.creating_order", nil))
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	h.stateManager.SetState(chatID, states.UserRenewSubWaitTariff, flowData)
	return h.showTariffs(chatID, flowData.Language)
}

// showTariffs shows available tariffs for renewal
func (h *Handler) showTariffs(chatID int64, lang string) error {
	ctx := context.Background()
	tariffs, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("get active tariffs: %w", err)
	}

	if len(tariffs) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "tariffs.no_active", nil))
		_, err = h.bot.Send(msg)
		return err
	}

	keyboard := h.createTariffsKeyboard(tariffs, lang)
	msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "tariffs.choose", nil))
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

// createTariffsKeyboard creates keyboard with tariffs
func (h *Handler) createTariffsKeyboard(tariffs []*tariffs.Tariff, lang string) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffs {
		durationText := h.formatDuration(t.DurationDays, lang)
		text := fmt.Sprintf("📅 %s - %.2f ₽", durationText, t.Price)
		callbackData := fmt.Sprintf("renew_tariff:%d:%.2f:%d", t.ID, t.Price, t.DurationDays)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(h.l10n.Get(lang, "buttons.cancel", nil), "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration форматирует длительность в удобный формат
func (h *Handler) formatDuration(days int, lang string) string {
	if days >= 365 {
		years := days / 365
		if years == 1 {
			return h.l10n.Get(lang, "tariffs.duration_1_year", nil)
		}
		return h.l10n.Get(lang, "tariffs.duration_years", map[string]interface{}{"years": years})
	}
	if days >= 30 {
		months := days / 30
		if months == 1 {
			return h.l10n.Get(lang, "tariffs.duration_1_month", nil)
		}
		return h.l10n.Get(lang, "tariffs.duration_months", map[string]interface{}{"months": months})
	}
	if days == 1 {
		return h.l10n.Get(lang, "tariffs.duration_1_day", nil)
	}
	return h.l10n.Get(lang, "tariffs.duration_days", map[string]interface{}{"days": days})
}

// handleTariffSelection handles tariff selection
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Получаем данные флоу для языка
	flowData, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, "ru", h.l10n.Get("ru", "flows.error_getting_data", nil))
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "flows.use_buttons", nil))
	}

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	tariffData, err := h.parseTariffFromCallback(update.CallbackQuery.Data)
	if err != nil {
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "renew.invalid_tariff", nil))
	}

	flowData.TariffID = tariffData.ID
	flowData.Price = tariffData.Price
	flowData.DurationDays = tariffData.DurationDays

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, h.l10n.Get(flowData.Language, "payment.creating_order", nil))
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	if tariffData.Price == 0 {
		return h.extendFreeSubscription(ctx, chatID, flowData)
	}

	h.stateManager.SetState(chatID, states.UserRenewSubWaitPayment, flowData)
	return h.createPaymentAndShow(ctx, chatID, flowData)
}

// TariffCallbackData represents parsed tariff callback data
type TariffCallbackData struct {
	ID           int64
	Price        float64
	DurationDays int
}

// parseTariffFromCallback parses tariff data from callback
func (h *Handler) parseTariffFromCallback(callbackData string) (*TariffCallbackData, error) {
	if !strings.HasPrefix(callbackData, "renew_tariff:") {
		return nil, fmt.Errorf("invalid callback format")
	}

	parts := strings.Split(callbackData, ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid tariff callback format")
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tariff ID: %w", err)
	}

	price, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tariff price: %w", err)
	}

	days, err := strconv.Atoi(parts[3])
	if err != nil {
		return nil, fmt.Errorf("invalid tariff duration: %w", err)
	}

	return &TariffCallbackData{
		ID:           id,
		Price:        price,
		DurationDays: days,
	}, nil
}

// createPaymentAndShow creates payment and shows payment link
func (h *Handler) createPaymentAndShow(ctx context.Context, chatID int64, data *flows.RenewSubFlowData) error {
	paymentEntity := payment.Payment{
		UserID: data.UserID,
		Amount: data.Price,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "payment.error_creating", nil))
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "payment.error_payment_url", nil))
	}

	data.PaymentID = &paymentObj.ID
	data.PaymentURL = paymentObj.PaymentURL

	// Проверяем, является ли это клиентской подпиской
	isClientSubscription := data.ClientName != nil && *data.ClientName != ""

	var paymentMsg string
	var keyboard tgbotapi.InlineKeyboardMarkup

	if isClientSubscription {
		// Для клиентских подписок показываем ссылку в тексте без кнопки оплаты
		paymentMsg = fmt.Sprintf(
			"💳 *Платеж создан!*\n\n"+
				"📋 Платеж #%d\n"+
				"👤 Клиент: %s\n"+
				"🔄 Продление на: %s\n"+
				"💰 Сумма: %.2f ₽\n\n"+
				"🔗 *Ссылка на оплату:*\n"+
				"%s\n\n"+
				"Отправьте эту ссылку клиенту.\n"+
				"После оплаты нажмите «Проверить оплату».",
			paymentObj.ID, *data.ClientName, h.formatDuration(data.DurationDays, data.Language), data.Price, *paymentObj.PaymentURL)

		checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", "payment_completed")
		cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_renewal")

		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(checkButton),
			tgbotapi.NewInlineKeyboardRow(cancelButton),
		)
	} else {
		// Для обычных подписок оставляем кнопку оплаты
		paymentMsg = fmt.Sprintf(
			"💳 *Платеж создан!*\n\n"+
				"📋 Платеж #%d\n"+
				"🔄 Продление на: %s\n"+
				"💰 Сумма: %.2f ₽\n\n"+
				"🔗 Перейдите по ссылке для оплаты.\n"+
				"После оплаты вернитесь сюда и нажмите «Оплатил».",
			paymentObj.ID, h.formatDuration(data.DurationDays, data.Language), data.Price)

		paymentButtonText := fmt.Sprintf("💳 Оплатить %.2f ₽", data.Price)
		paymentButton := tgbotapi.NewInlineKeyboardButtonURL(paymentButtonText, *paymentObj.PaymentURL)
		completeButton := tgbotapi.NewInlineKeyboardButtonData("✅ Оплатил", "payment_completed")
		cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_renewal")

		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(paymentButton),
			tgbotapi.NewInlineKeyboardRow(completeButton),
			tgbotapi.NewInlineKeyboardRow(cancelButton),
		)
	}

	msg := tgbotapi.NewMessage(chatID, paymentMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	if err != nil {
		return err
	}

	h.stateManager.SetState(chatID, states.UserRenewSubWaitPayment, data)
	return nil
}

// handlePaymentConfirmation handles payment confirmation
func (h *Handler) handlePaymentConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	data, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, "ru", h.l10n.Get("ru", "flows.error_getting_data", nil))
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "flows.use_buttons", nil))
	}

	callbackData := update.CallbackQuery.Data

	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "cancel_renewal" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "flows.unknown_command", nil))
	}
}

// handlePaymentCompleted handles payment completion
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.RenewSubFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, h.l10n.Get(data.Language, "payment.checking", nil))
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	if data.PaymentID == nil {
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "payment.not_found", nil))
	}

	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, *data.PaymentID)
	if err != nil {
		return h.sendPaymentCheckError(chatID, data, h.l10n.Get(data.Language, "payment.error_checking", nil))
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		return h.sendPaymentPendingMessage(chatID, data)
	case payment.StatusRejected, payment.StatusCancelled:
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "payment.rejected", nil))
	default:
		return h.sendPaymentCheckError(chatID, data, h.l10n.Get(data.Language, "payment.unknown_status", nil))
	}
}

// handleSuccessfulPayment handles successful payment and extends subscription
func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, data *flows.RenewSubFlowData, paymentID int64) error {
	err := h.subscriptionService.ExtendSubscription(ctx, data.SubscriptionID, data.DurationDays)
	if err != nil {
		h.logger.Error("Failed to extend subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "renew.error_renewing", nil))
	}

	err = h.paymentService.LinkPaymentToSubscriptions(ctx, paymentID, []int64{data.SubscriptionID})
	if err != nil {
		h.logger.Warn("Failed to link payment to subscription",
			"error", err,
			"payment_id", paymentID,
			"subscription_id", data.SubscriptionID)
	}

	subscription, err := h.subscriptionService.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{data.SubscriptionID}})
	if err != nil {
		h.logger.Error("Failed to get subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "flows.error_getting_data", nil))
	}

	err = h.sendSuccessMessage(chatID, subscription, data)
	if err != nil {
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "subscription.error_sending_instructions", nil))
	}

	h.stateManager.Clear(chatID)
	return nil
}

// sendSuccessMessage sends success message
func (h *Handler) sendSuccessMessage(chatID int64, subscription *subs.Subscription, data *flows.RenewSubFlowData) error {
	expiresText := "неизвестно"
	if subscription.ExpiresAt != nil {
		expiresText = subscription.ExpiresAt.Format("02.01.2006 15:04")
	}

	messageText := h.l10n.Get(data.Language, "renew.success", map[string]interface{}{
		"subscription_id": subscription.ID,
		"days_added":      data.DurationDays,
		"expires_at":      expiresText,
		"marzban_link":    subscription.MarzbanLink,
	})

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"

	_, err := h.bot.Send(msg)
	return err
}

// sendPaymentPendingMessage sends message about pending payment
func (h *Handler) sendPaymentPendingMessage(chatID int64, data *flows.RenewSubFlowData) error {
	msg := tgbotapi.NewMessage(chatID,
		"⏳ Платеж еще обрабатывается.\n"+
			"Пожалуйста, подождите немного и попробуйте еще раз.")

	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_renewal")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendPaymentCheckError sends payment check error message
func (h *Handler) sendPaymentCheckError(chatID int64, data *flows.RenewSubFlowData, errorMsg string) error {
	msg := tgbotapi.NewMessage(chatID, errorMsg)

	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_renewal")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(retryButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// extendFreeSubscription extends subscription for free
func (h *Handler) extendFreeSubscription(ctx context.Context, chatID int64, data *flows.RenewSubFlowData) error {
	err := h.subscriptionService.ExtendSubscription(ctx, data.SubscriptionID, data.DurationDays)
	if err != nil {
		h.logger.Error("Failed to extend subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "renew.error_renewing", nil))
	}

	subscription, err := h.subscriptionService.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{data.SubscriptionID}})
	if err != nil {
		h.logger.Error("Failed to get subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "flows.error_getting_data", nil))
	}

	err = h.sendSuccessMessage(chatID, subscription, data)
	if err != nil {
		return h.sendError(chatID, data.Language, h.l10n.Get(data.Language, "subscription.error_sending_instructions", nil))
	}

	h.stateManager.Clear(chatID)
	return nil
}

// handleCancel handles cancellation
func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Возвращаемся в главное меню")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	return h.sendMainMenu(chatID)
}

// sendMainMenu sends main menu
func (h *Handler) sendMainMenu(chatID int64) error {
	text := "Доступные команды:\n" +
		"/start — Начать работу\n" +
		"/buy — Купить ключ доступа\n" +
		"/renew — Продлить подписку"

	msg := tgbotapi.NewMessage(chatID, text)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) sendError(chatID int64, lang, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := h.bot.Send(msg)
	return err
}

func extractChatID(update *tgbotapi.Update) int64 {
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	if update.CallbackQuery != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}
