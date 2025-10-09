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
	flowData := &flows.RenewSubFlowData{
		UserID: userID,
	}
	h.stateManager.SetState(chatID, states.UserRenewSubWaitSelection, flowData)

	return h.showSubscriptions(chatID, userID)
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
func (h *Handler) showSubscriptions(chatID, userID int64) error {
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
		msg := tgbotapi.NewMessage(chatID, "❌ У вас нет подписок для продления")
		_, err = h.bot.Send(msg)
		return err
	}

	keyboard := h.createSubscriptionsKeyboard(subscriptions)
	msg := tgbotapi.NewMessage(chatID, "🔄 Выберите подписку для продления:")
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

// createSubscriptionsKeyboard creates keyboard with subscriptions
func (h *Handler) createSubscriptionsKeyboard(subscriptions []*subs.Subscription) tgbotapi.InlineKeyboardMarkup {
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

		text := fmt.Sprintf("%s Подписка #%d (до %s)", statusEmoji, sub.ID, expiresText)
		callbackData := fmt.Sprintf("renew_sub:%d", sub.ID)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// handleSubscriptionSelection handles subscription selection
func (h *Handler) handleSubscriptionSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		return h.sendError(chatID, "Пожалуйста, выберите подписку из меню")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if !strings.HasPrefix(update.CallbackQuery.Data, "renew_sub:") {
		return h.sendError(chatID, "Неверные данные")
	}

	parts := strings.Split(update.CallbackQuery.Data, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, "Неверный формат данных")
	}

	subID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID подписки")
	}

	flowData, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	flowData.SubscriptionID = subID

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Выбираем тариф...")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	h.stateManager.SetState(chatID, states.UserRenewSubWaitTariff, flowData)
	return h.showTariffs(chatID)
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
		msg := tgbotapi.NewMessage(chatID, "❌ К сожалению, активных тарифов сейчас нет")
		_, err = h.bot.Send(msg)
		return err
	}

	keyboard := h.createTariffsKeyboard(tariffs)
	msg := tgbotapi.NewMessage(chatID, "📅 Выберите период продления:")
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
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
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration formats duration in a user-friendly format
func formatDuration(days int) string {
	if days >= 365 {
		years := days / 365
		if years == 1 {
			return "1 год"
		}
		return fmt.Sprintf("%d лет", years)
	}
	if days >= 30 {
		months := days / 30
		if months == 1 {
			return "1 месяц"
		}
		return fmt.Sprintf("%d мес", months)
	}
	if days == 1 {
		return "1 день"
	}
	return fmt.Sprintf("%d дней", days)
}

// handleTariffSelection handles tariff selection
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		return h.sendError(chatID, "Пожалуйста, выберите тариф из меню")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	tariffData, err := h.parseTariffFromCallback(update.CallbackQuery.Data)
	if err != nil {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	flowData, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	flowData.TariffID = tariffData.ID
	flowData.Price = tariffData.Price
	flowData.DurationDays = tariffData.DurationDays

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём платеж...")
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
		return h.sendError(chatID, "❌ Ошибка создания платежа")
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "❌ Ошибка генерации ссылки на оплату")
	}

	data.PaymentID = &paymentObj.ID
	data.PaymentURL = paymentObj.PaymentURL

	paymentMsg := fmt.Sprintf(
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

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(paymentButton),
		tgbotapi.NewInlineKeyboardRow(completeButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

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
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	data, err := h.stateManager.GetRenewSubData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "cancel_renewal" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
}

// handlePaymentCompleted handles payment completion
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.RenewSubFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Проверяем платеж...")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	if data.PaymentID == nil {
		return h.sendError(chatID, "❌ Ошибка: платеж не найден")
	}

	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, *data.PaymentID)
	if err != nil {
		return h.sendPaymentCheckError(chatID, data, "❌ Ошибка проверки платежа. Попробуйте еще раз.")
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		return h.sendPaymentPendingMessage(chatID, data)
	case payment.StatusRejected, payment.StatusCancelled:
		return h.sendError(chatID, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendPaymentCheckError(chatID, data, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// handleSuccessfulPayment handles successful payment and extends subscription
func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, data *flows.RenewSubFlowData, paymentID int64) error {
	err := h.subscriptionService.ExtendSubscription(ctx, data.SubscriptionID, data.DurationDays)
	if err != nil {
		h.logger.Error("Failed to extend subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, "❌ Ошибка продления подписки")
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
		return h.sendError(chatID, "❌ Ошибка получения данных подписки")
	}

	err = h.sendSuccessMessage(chatID, subscription, data.DurationDays)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка отправки подтверждения")
	}

	h.stateManager.Clear(chatID)
	return nil
}

// sendSuccessMessage sends success message
func (h *Handler) sendSuccessMessage(chatID int64, subscription *subs.Subscription, daysAdded int) error {
	expiresText := "неизвестно"
	if subscription.ExpiresAt != nil {
		expiresText = subscription.ExpiresAt.Format("02.01.2006 15:04")
	}

	messageText := fmt.Sprintf(
		"✅ *Подписка успешно продлена!*\n\n"+
			"🔄 Добавлено дней: %d\n"+
			"📅 Новая дата окончания: %s\n\n"+
			"🔗 Ваша ссылка подключения осталась прежней:\n"+
			"`%s`\n\n"+
			"Спасибо за продление!",
		daysAdded, expiresText, subscription.MarzbanLink)

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
		return h.sendError(chatID, "❌ Ошибка продления подписки")
	}

	subscription, err := h.subscriptionService.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{data.SubscriptionID}})
	if err != nil {
		h.logger.Error("Failed to get subscription", "error", err, "subscription_id", data.SubscriptionID)
		return h.sendError(chatID, "❌ Ошибка получения данных подписки")
	}

	err = h.sendSuccessMessage(chatID, subscription, data.DurationDays)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка отправки подтверждения")
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

func (h *Handler) sendError(chatID int64, message string) error {
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
