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
	"kurut-bot/internal/telegram/messages"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot                 botApi
	stateManager        stateManager
	subscriptionService subscriptionService
	tariffService       tariffService
	paymentService      paymentService
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ss subscriptionService,
	ts tariffService,
	ps paymentService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		subscriptionService: ss,
		tariffService:       ts,
		paymentService:      ps,
		logger:              logger,
	}
}

// Start starts the renewal flow
func (h *Handler) Start(userID, chatID int64) error {
	ctx := context.Background()

	subscriptions, err := h.subscriptionService.ListSubscriptions(ctx, subs.ListCriteria{
		UserIDs: []int64{userID},
		Status:  []subs.Status{subs.StatusActive, subs.StatusExpired},
	})
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, messages.RenewNoSubscriptions)
		_, _ = h.bot.Send(msg)
		return fmt.Errorf("list subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, messages.RenewNoSubscriptions)
		_, err = h.bot.Send(msg)
		return err
	}

	// If only one subscription - show quick renew options
	if len(subscriptions) == 1 {
		return h.showQuickRenewOptions(chatID, userID, subscriptions[0])
	}

	// If multiple subscriptions - show selection
	flowData := &flows.RenewSubFlowData{
		UserID: userID,
		Page:   0,
	}
	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	return h.showSubscriptions(chatID, userID, 0)
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
func (h *Handler) showQuickRenewOptions(chatID, userID int64, subscription *subs.Subscription) error {
	// Prepare flow data
	flowData := &flows.RenewSubFlowData{
		UserID:         userID,
		SubscriptionID: subscription.ID,
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
		return h.sendError(chatID, messages.RenewErrorLoadingTariff)
	}

	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	// Format expiration date
	expiresText := "неизвестно"
	if subscription.ExpiresAt != nil {
		expiresText = subscription.ExpiresAt.Format("02.01.2006")
	}

	// Build message
	messageText := messages.FormatRenewQuickTitle(subscription.ID, tariff.Name, expiresText)

	// Create keyboard with two options
	var rows [][]tgbotapi.InlineKeyboardButton

	// Button 1: Renew with same tariff
	durationText := formatDuration(tariff.DurationDays)
	quickRenewText := messages.FormatRenewQuickSame(durationText, tariff.Price)
	quickRenewCallback := fmt.Sprintf("renew_quick:%d:%.2f:%d", tariff.ID, tariff.Price, tariff.DurationDays)
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⚡️ "+quickRenewText, quickRenewCallback),
	})

	// Button 2: Choose different tariff
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("📋 "+messages.RenewChooseDifferentTariff, "renew_choose_tariff"),
	})

	// Cancel button
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancel, "cancel"),
	})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard

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
func (h *Handler) showSubscriptions(chatID, userID int64, page int) error {
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
		msg := tgbotapi.NewMessage(chatID, messages.RenewNoSubscriptions)
		_, err = h.bot.Send(msg)
		return err
	}

	keyboard, err := h.createSubscriptionsKeyboard(ctx, subscriptions, page)
	if err != nil {
		return fmt.Errorf("create subscriptions keyboard: %w", err)
	}

	msg := tgbotapi.NewMessage(chatID, messages.RenewChooseSubscription)
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
func (h *Handler) createSubscriptionsKeyboard(ctx context.Context, subscriptions []*subs.Subscription, page int) (tgbotapi.InlineKeyboardMarkup, error) {
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

		text := messages.FormatRenewSubscriptionButton(sub.ID, tariffName, expiresText)
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
		tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancel, "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

// handleSubscriptionSelection handles subscription selection
func (h *Handler) handleSubscriptionSelection(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Получаем данные флоу для языка
	flowData, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, messages.FlowUseButtons)
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
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.TariffsChoose)
		_, _ = h.bot.Request(callbackConfig)
		h.stateManager.SetState(chatID, states.UserRenewSubWaitTariff, flowData)
		return h.showTariffs(chatID)
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

		return h.showSubscriptions(chatID, flowData.UserID, page)
	}

	// Handle noop (page indicator button)
	if callbackData == "renew_noop" {
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
		_, _ = h.bot.Request(callbackConfig)
		return nil
	}

	if !strings.HasPrefix(callbackData, "renew_sub:") {
		return h.sendError(chatID, messages.RenewInvalidSubscription)
	}

	parts := strings.Split(update.CallbackQuery.Data, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, messages.RenewInvalidSubscription)
	}

	subID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, messages.RenewInvalidSubscription)
	}

	flowData.SubscriptionID = subID

	// Получаем информацию о подписке, чтобы проверить, есть ли client_name
	subscription, err := h.subscriptionService.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{subID}})
	if err != nil {
		return h.sendError(chatID, messages.FlowErrorGettingData)
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
func (h *Handler) showTariffs(chatID int64) error {
	ctx := context.Background()
	tariffs, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("get active tariffs: %w", err)
	}

	if len(tariffs) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, messages.TariffsNoActive)
		_, err = h.bot.Send(msg)
		return err
	}

	// Получаем данные флоу
	flowData, _ := h.stateManager.GetRenewSubData(chatID)

	keyboard := h.createTariffsKeyboard(tariffs)
	messageText := messages.TariffsChoose

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
func (h *Handler) createTariffsKeyboard(tariffs []*tariffs.Tariff) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffs {
		durationText := formatDuration(t.DurationDays)
		text := fmt.Sprintf("📅 %s - %.2f ₽", durationText, t.Price)
		callbackData := fmt.Sprintf("renew_tariff:%d:%.2f:%d", t.ID, t.Price, t.DurationDays)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancel, "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration форматирует длительность в удобный формат
func formatDuration(days int) string {
	if days >= 365 {
		years := days / 365
		if years == 1 {
			return messages.FormatDuration1Year()
		}
		return messages.FormatDurationYears(years)
	}
	if days >= 30 {
		months := days / 30
		if months == 1 {
			return messages.FormatDuration1Month()
		}
		return messages.FormatDurationMonths(months)
	}
	if days == 1 {
		return messages.FormatDuration1Day()
	}
	return messages.FormatDurationDays(days)
}

// handleTariffSelection handles tariff selection
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Получаем данные флоу для языка
	flowData, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, messages.FlowUseButtons)
	}

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	tariffData, err := h.parseTariffFromCallback(update.CallbackQuery.Data)
	if err != nil {
		return h.sendError(chatID, messages.RenewInvalidTariff)
	}

	flowData.TariffID = tariffData.ID
	flowData.Price = tariffData.Price
	flowData.DurationDays = tariffData.DurationDays

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.PaymentCreating)
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
		return h.sendError(chatID, messages.RenewInvalidTariff)
	}

	flowData.TariffID = tariffData.ID
	flowData.Price = tariffData.Price
	flowData.DurationDays = tariffData.DurationDays

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.PaymentCreating)
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
		return h.sendError(chatID, messages.PaymentErrorCreating)
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, messages.PaymentErrorPaymentURL)
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
			paymentObj.ID, *data.ClientName, formatDuration(data.DurationDays), data.Price, *paymentObj.PaymentURL)

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
			paymentObj.ID, formatDuration(data.DurationDays), data.Price)

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
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		if err != nil {
			return err
		}
	} else {
		// Fallback: отправляем новое сообщение
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
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
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, messages.FlowUseButtons)
	}

	callbackData := update.CallbackQuery.Data

	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "cancel_renewal" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, messages.FlowUnknownCommand)
	}
}

// handlePaymentCompleted handles payment completion
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.RenewSubFlowData) error {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.PaymentChecking)
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	if data.PaymentID == nil {
		return h.sendError(chatID, messages.PaymentNotFound)
	}

	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, *data.PaymentID)
	if err != nil {
		return h.sendPaymentCheckError(chatID, data, messages.PaymentErrorChecking)
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		return h.sendPaymentPendingMessage(chatID, data)
	case payment.StatusRejected, payment.StatusCancelled:
		return h.sendError(chatID, messages.PaymentRejected)
	default:
		return h.sendPaymentCheckError(chatID, data, messages.PaymentUnknownStatus)
	}
}

// handleSuccessfulPayment handles successful payment and extends subscription
func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, data *flows.RenewSubFlowData, paymentID int64) error {
	err := h.subscriptionService.ExtendSubscription(ctx, data.SubscriptionID, data.DurationDays)
	if err != nil {
		h.logger.Error("Failed to extend subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, messages.RenewErrorRenewing)
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
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	err = h.sendSuccessMessage(chatID, subscription, data)
	if err != nil {
		return h.sendError(chatID, messages.SubscriptionErrorSendingInstructions)
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

	messageText := messages.FormatRenewSuccess(subscription.ID, data.DurationDays, expiresText)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ReplyMarkup = nil // Убираем кнопки
		_, err := h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, messageText)
	_, err := h.bot.Send(msg)
	return err
}

// sendPaymentPendingMessage sends message about pending payment
func (h *Handler) sendPaymentPendingMessage(chatID int64, data *flows.RenewSubFlowData) error {
	messageText := messages.PaymentPending

	checkButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCheckAgain, "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancelPurchase, "cancel_renewal")

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
	retryButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonRetry, "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancelPurchase, "cancel_renewal")

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
		return h.sendError(chatID, messages.RenewErrorRenewing)
	}

	subscription, err := h.subscriptionService.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{data.SubscriptionID}})
	if err != nil {
		h.logger.Error("Failed to get subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	err = h.sendSuccessMessage(chatID, subscription, data)
	if err != nil {
		return h.sendError(chatID, messages.SubscriptionErrorSendingInstructions)
	}

	h.stateManager.Clear(chatID)
	return nil
}

// handleCancel handles cancellation
func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.FlowReturningToMenu)
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	return h.sendMainMenu(chatID)
}

// sendMainMenu sends main menu
func (h *Handler) sendMainMenu(chatID int64) error {
	msg := tgbotapi.NewMessage(chatID, messages.CommandsHelp)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, message)
	_, err := h.bot.Send(msg)
	return err
}

func extractChatID(update *tgbotapi.Update) int64 {
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}
