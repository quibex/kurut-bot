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
	ctx := context.Background()

	subscriptions, err := h.subscriptionService.ListSubscriptions(ctx, subs.ListCriteria{
		UserIDs: []int64{userID},
		Status:  []subs.Status{subs.StatusActive, subs.StatusExpired},
	})
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "renew.no_subscriptions", nil))
		_, _ = h.bot.Send(msg)
		return fmt.Errorf("list subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "renew.no_subscriptions", nil))
		_, err = h.bot.Send(msg)
		return err
	}

	// If only one subscription - show quick renew options
	if len(subscriptions) == 1 {
		return h.showQuickRenewOptions(chatID, userID, subscriptions[0], lang)
	}

	// If multiple subscriptions - show selection
	flowData := &flows.RenewSubFlowData{
		UserID:   userID,
		Language: lang,
		Page:     0,
	}
	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	return h.showSubscriptions(chatID, userID, lang, 0)
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

// showQuickRenewOptions shows quick renewal options when there's only one subscription
func (h *Handler) showQuickRenewOptions(chatID, userID int64, subscription *subs.Subscription, lang string) error {
	// Prepare flow data
	flowData := &flows.RenewSubFlowData{
		UserID:         userID,
		SubscriptionID: subscription.ID,
		Language:       lang,
		Page:           0,
	}

	// Save client_name if exists
	if subscription.ClientName != nil {
		flowData.ClientName = subscription.ClientName
	}

	return h.showQuickRenewOptionsWithData(chatID, subscription, flowData)
}

// showQuickRenewOptionsWithData shows quick renewal options using existing flow data
func (h *Handler) showQuickRenewOptionsWithData(chatID int64, subscription *subs.Subscription, flowData *flows.RenewSubFlowData) error {
	ctx := context.Background()

	// Get subscription tariff
	tariff, err := h.tariffService.GetTariff(ctx, tariffs.GetCriteria{ID: &subscription.TariffID})
	if err != nil {
		h.logger.Error("Failed to get tariff", "error", err, "tariff_id", subscription.TariffID)
		return h.sendError(chatID, flowData.Language, h.l10n.Get(flowData.Language, "renew.error_loading_tariff", nil))
	}

	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	// Format expiration date
	expiresText := "неизвестно"
	if subscription.ExpiresAt != nil {
		expiresText = subscription.ExpiresAt.Format("02.01.2006")
	}

	// Build message
	messageText := h.l10n.Get(flowData.Language, "renew.quick_renew_title", map[string]interface{}{
		"id":          subscription.ID,
		"tariff_name": tariff.Name,
		"expires_at":  expiresText,
	})

	// Create keyboard with two options
	var rows [][]tgbotapi.InlineKeyboardButton

	// Button 1: Renew with same tariff
	durationText := h.formatDuration(tariff.DurationDays, flowData.Language)
	quickRenewText := h.l10n.Get(flowData.Language, "renew.quick_renew_same", map[string]interface{}{
		"duration": durationText,
		"price":    tariff.Price,
	})
	quickRenewCallback := fmt.Sprintf("renew_quick:%d:%.2f:%d", tariff.ID, tariff.Price, tariff.DurationDays)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⚡️ "+quickRenewText, quickRenewCallback),
	})

	// Button 2: Choose different tariff
	chooseTariffText := h.l10n.Get(flowData.Language, "renew.choose_different_tariff", nil)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("📋 "+chooseTariffText, "renew_choose_tariff"),
	})

	// Cancel button
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(h.l10n.Get(flowData.Language, "buttons.cancel", nil), "cancel"),
	})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Save message ID
	flowData.MessageID = &sentMsg.MessageID
	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	return nil
}

// showSubscriptions shows user's active and expired subscriptions
func (h *Handler) showSubscriptions(chatID, userID int64, lang string, page int) error {
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

	keyboard, err := h.createSubscriptionsKeyboard(ctx, subscriptions, lang, page)
	if err != nil {
		return fmt.Errorf("create subscriptions keyboard: %w", err)
	}

	msg := tgbotapi.NewMessage(chatID, h.l10n.Get(lang, "renew.choose_subscription", nil))
	msg.ReplyMarkup = keyboard

	// Отправляем сообщение и сохраняем его ID
	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Сохраняем MessageID
	flowData, _ := h.stateManager.GetRenewSubData(chatID)
	if flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)
	}

	return nil
}

// createSubscriptionsKeyboard creates keyboard with subscriptions
func (h *Handler) createSubscriptionsKeyboard(ctx context.Context, subscriptions []*subs.Subscription, lang string, page int) (tgbotapi.InlineKeyboardMarkup, error) {
	const pageSize = 5
	var rows [][]tgbotapi.InlineKeyboardButton

	// Calculate pagination
	totalPages := (len(subscriptions) + pageSize - 1) / pageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	startIdx := page * pageSize
	endIdx := startIdx + pageSize
	if endIdx > len(subscriptions) {
		endIdx = len(subscriptions)
	}

	// Show subscriptions for current page
	for i := startIdx; i < endIdx; i++ {
		sub := subscriptions[i]
		expiresText := "неизвестно"
		statusEmoji := "🔑"

		if sub.ExpiresAt != nil {
			expiresText = sub.ExpiresAt.Format("02.01.2006")
		}

		if sub.Status == subs.StatusExpired {
			statusEmoji = "❌"
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

	// Add navigation buttons if needed
	if totalPages > 1 {
		var navButtons []tgbotapi.InlineKeyboardButton
		if page > 0 {
			navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️", fmt.Sprintf("renew_page:%d", page-1)))
		}
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "renew_noop"))
		if page < totalPages-1 {
			navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("➡️", fmt.Sprintf("renew_page:%d", page+1)))
		}
		rows = append(rows, navButtons)
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

	callbackData := update.CallbackQuery.Data

	if callbackData == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Handle quick renew with same tariff
	if strings.HasPrefix(callbackData, "renew_quick:") {
		return h.handleQuickRenew(ctx, update, flowData)
	}

	// Handle choose different tariff
	if callbackData == "renew_choose_tariff" {
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, h.l10n.Get(flowData.Language, "tariffs.choose", nil))
		_, _ = h.bot.Request(callbackConfig)
		h.stateManager.SetState(chatID, states.UserRenewSubWaitTariff, flowData)
		return h.showTariffs(chatID, flowData.Language)
	}

	// Handle pagination
	if strings.HasPrefix(callbackData, "renew_page:") {
		parts := strings.Split(callbackData, ":")
		if len(parts) != 2 {
			return nil
		}
		page, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		flowData.Page = page
		h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

		// Delete old message
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, update.CallbackQuery.Message.MessageID)
		_, _ = h.bot.Request(deleteMsg)

		return h.showSubscriptions(chatID, flowData.UserID, flowData.Language, page)
	}

	// Handle noop (page indicator button)
	if callbackData == "renew_noop" {
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
		_, _ = h.bot.Request(callbackConfig)
		return nil
	}

	if !strings.HasPrefix(callbackData, "renew_sub:") {
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

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
	_, _ = h.bot.Request(callbackConfig)

	// Delete the subscription list message
	if flowData.MessageID != nil {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, *flowData.MessageID)
		_, _ = h.bot.Request(deleteMsg)
	}

	// Show quick renew options for selected subscription
	return h.showQuickRenewOptionsWithData(chatID, subscription, flowData)
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

	// Получаем данные флоу
	flowData, _ := h.stateManager.GetRenewSubData(chatID)

	keyboard := h.createTariffsKeyboard(tariffs, lang)
	messageText := h.l10n.Get(lang, "tariffs.choose", nil)

	// Редактируем существующее сообщение, если MessageID есть
	if flowData != nil && flowData.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, messageText)
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err == nil && flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.UserRenewSubWaitTariff, flowData)
	}
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

// handleQuickRenew handles quick renewal with the same tariff
func (h *Handler) handleQuickRenew(ctx context.Context, update *tgbotapi.Update, flowData *flows.RenewSubFlowData) error {
	chatID := extractChatID(update)

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
	// Support both "renew_tariff:" and "renew_quick:" formats
	if !strings.HasPrefix(callbackData, "renew_tariff:") && !strings.HasPrefix(callbackData, "renew_quick:") {
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

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		if err != nil {
			return err
		}
	} else {
		// Fallback: отправляем новое сообщение
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		sentMsg, err := h.bot.Send(msg)
		if err != nil {
			return err
		}
		data.MessageID = &sentMsg.MessageID
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
	})

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = nil // Убираем кнопки
		_, err := h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)
	return err
}

// sendPaymentPendingMessage sends message about pending payment
func (h *Handler) sendPaymentPendingMessage(chatID int64, data *flows.RenewSubFlowData) error {
	messageText := "⏳ Платеж еще обрабатывается.\n" +
		"Пожалуйста, подождите немного и попробуйте еще раз."

	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_renewal")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err == nil {
		data.MessageID = &sentMsg.MessageID
	}
	return err
}

// sendPaymentCheckError sends payment check error message
func (h *Handler) sendPaymentCheckError(chatID int64, data *flows.RenewSubFlowData, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_renewal")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(retryButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, errorMsg)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, errorMsg)
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err == nil {
		data.MessageID = &sentMsg.MessageID
	}
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
