package buysub

import (
	"context"
	"encoding/base64"
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
	tariffService       tariffService
	subscriptionService subscriptionService
	paymentService      paymentService
	storage             storageService
	configStore         configStore
	webAppBaseURL       string
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	ss subscriptionService,
	ps paymentService,
	storage storageService,
	configStore configStore,
	webAppBaseURL string,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		subscriptionService: ss,
		paymentService:      ps,
		storage:             storage,
		configStore:         configStore,
		webAppBaseURL:       webAppBaseURL,
		logger:              logger,
	}
}

// Start начинает flow покупки подписки
func (h *Handler) Start(userID, chatID int64, messageID *int) error {
	// Инициализируем данные флоу с внутренним ID пользователя
	flowData := &flows.BuySubFlowData{
		UserID:    userID,
		MessageID: messageID,
	}
	h.stateManager.SetState(chatID, states.UserBuySubWaitTariff, flowData)

	// Показываем тарифы
	return h.showTariffs(chatID)
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.UserBuySubWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.UserBuySubWaitPayment:
		return h.handlePaymentConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown state: %s", state)
	}
}

func (h *Handler) showTariffs(chatID int64) error {
	ctx := context.Background()
	tariffs, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения тарифов: %w", err)
	}

	if len(tariffs) == 0 {
		// Очищаем состояние пользователя, чтобы он вышел из flow
		h.stateManager.Clear(chatID)

		msg := tgbotapi.NewMessage(chatID, messages.TariffsNoActive)
		_, err = h.bot.Send(msg)
		return err
	}

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffs)

	msg := tgbotapi.NewMessage(chatID, messages.TariffsChoose)
	msg.ReplyMarkup = keyboard

	// Отправляем сообщение и сохраняем его ID
	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Получаем текущие данные флоу и обновляем MessageID
	flowData, _ := h.stateManager.GetBuySubData(chatID)
	if flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.UserBuySubWaitTariff, flowData)
	}

	return nil
}

// handleTariffSelection обработка выбора тарифа
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		// Получаем язык из flow data
		flowData, err := h.stateManager.GetBuySubData(chatID)
		if err != nil {
			return h.sendError(chatID, messages.FlowErrorGettingData)
		}
		// Проверяем есть ли активные тарифы, если нет - выходим из flow
		tariffs, err := h.tariffService.GetActiveTariffs(ctx)
		if err == nil && len(tariffs) == 0 {
			h.stateManager.Clear(chatID)
			return h.sendError(chatID, messages.TariffsNoActive)
		}
		_ = flowData // unused but kept for context
		return h.sendError(chatID, messages.TariffsPleaseSelect)
	}

	if update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID

	// Проверяем на отмену
	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Получаем существующие данные флоу, чтобы сохранить UserID и язык
	flowData, err := h.stateManager.GetBuySubData(chatID)
	if err != nil {
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	// Парсим данные тарифа
	tariffData, err := h.parseTariffFromCallback(update.CallbackQuery.Data)
	if err != nil {
		return h.sendError(chatID, messages.TariffsInvalidData)
	}

	// Обновляем данные о тарифе
	flowData.TariffID = tariffData.ID
	flowData.TariffName = tariffData.Name
	flowData.Price = tariffData.Price
	flowData.TotalAmount = tariffData.Price

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.PaymentCreating)
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Check if WireGuard servers are available before creating payment
	servers, err := h.storage.ListEnabledWGServers(ctx)
	if err != nil {
		h.logger.Error("Failed to check WireGuard servers", "error", err)
		return h.sendError(chatID, messages.SubscriptionErrorServerCheck)
	}

	if len(servers) == 0 {
		h.logger.Warn("No WireGuard servers available for subscription")
		h.stateManager.Clear(chatID)
		return h.sendError(chatID, messages.SubscriptionNoServersAvailable)
	}

	// Check if any server has capacity
	hasCapacity := false
	for _, server := range servers {
		if server.CurrentPeers < server.MaxPeers {
			hasCapacity = true
			break
		}
	}

	if !hasCapacity {
		h.logger.Warn("All WireGuard servers at capacity")
		h.stateManager.Clear(chatID)
		return h.sendError(chatID, messages.SubscriptionServersAtCapacity)
	}

	// Если тариф бесплатный - сразу создаем подписку без оплаты
	if tariffData.Price == 0 {
		return h.createFreeSubscription(ctx, chatID, flowData)
	}

	// Переводим в состояние ожидания оплаты
	h.stateManager.SetState(chatID, states.UserBuySubWaitPayment, flowData)

	// Сразу создаём платёж и показываем ссылку на оплату
	return h.createPaymentAndShow(ctx, chatID, flowData)
}

// handlePaymentConfirmation обработка подтверждения оплаты
func (h *Handler) handlePaymentConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Получаем данные флоу
	data, err := h.stateManager.GetBuySubData(chatID)
	if err != nil {
		return h.sendError(chatID, messages.FlowErrorGettingData)
	}

	if update.CallbackQuery == nil {
		return h.sendError(chatID, messages.FlowUseButtons)
	}

	callbackData := update.CallbackQuery.Data

	// Обрабатываем разные типы callback
	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "cancel_purchase" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, messages.FlowUnknownCommand)
	}
}

// createPaymentAndShow создает платеж и сразу показывает ссылку на оплату (без промежуточных этапов)
func (h *Handler) createPaymentAndShow(ctx context.Context, chatID int64, data *flows.BuySubFlowData) error {
	// Создаем платеж
	paymentEntity := payment.Payment{
		UserID: data.UserID,
		Amount: data.TotalAmount,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		return h.sendError(chatID, messages.PaymentErrorCreating)
	}

	// Проверяем что ссылка на оплату была создана
	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, messages.PaymentErrorPaymentURL)
	}

	// Сохраняем данные платежа в флоу
	data.PaymentID = &paymentObj.ID
	data.PaymentURL = paymentObj.PaymentURL

	// Показываем сообщение с ссылкой на оплату
	paymentMsg := messages.FormatPaymentOrderCreated(paymentObj.ID, data.TariffName, data.TotalAmount)

	// Создаем ссылку для оплаты
	paymentButtonText := messages.FormatPayButtonText(data.TotalAmount)
	paymentButton := tgbotapi.NewInlineKeyboardButtonURL(paymentButtonText, *paymentObj.PaymentURL)
	completeButton := tgbotapi.NewInlineKeyboardButtonData(
		messages.ButtonPaid,
		"payment_completed",
	)
	cancelButton := tgbotapi.NewInlineKeyboardButtonData(
		messages.ButtonCancelPurchase,
		"cancel_purchase",
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(paymentButton),
		tgbotapi.NewInlineKeyboardRow(completeButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, paymentMsg)
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		if err != nil {
			return err
		}
	} else {
		// Fallback: отправляем новое сообщение, если MessageID нет
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
		msg.ReplyMarkup = keyboard
		sentMsg, err := h.bot.Send(msg)
		if err != nil {
			return err
		}
		data.MessageID = &sentMsg.MessageID
	}

	// Сохраняем обновленное состояние с данными платежа
	h.stateManager.SetState(chatID, states.UserBuySubWaitPayment, data)

	return nil
}

// handleCancel обрабатывает отмену любого действия и возвращает в главное меню
func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.FlowReturningToMenu)
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем главное меню
	return h.sendMainMenu(chatID)
}

// sendMainMenu отправляет главное меню
func (h *Handler) sendMainMenu(chatID int64) error {
	msg := tgbotapi.NewMessage(chatID, messages.CommandsHelp)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) createTariffsKeyboard(tariffs []*tariffs.Tariff) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffs {
		durationText := formatDuration(t.DurationDays)
		text := fmt.Sprintf("📅 %s - %.2f ₽ (%s)", t.Name, t.Price, durationText)
		callbackData := fmt.Sprintf("tariff:%d:%.2f:%s:%d", t.ID, t.Price, t.Name, t.DurationDays)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Добавляем кнопку отмены
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancel, "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration форматирует длительность в удобный формат (дни/месяцы/годы)
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

// handlePaymentCompleted обрабатывает нажатие кнопки "Оплатил"
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.BuySubFlowData) error {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return nil
	}
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, messages.PaymentChecking)
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Проверяем что paymentID есть
	if data.PaymentID == nil {
		return h.sendError(chatID, messages.PaymentNotFound)
	}

	// Проверяем статус платежа через API
	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, *data.PaymentID)
	if err != nil {
		return h.sendPaymentCheckError(chatID, data, messages.PaymentErrorChecking)
	}

	// Проверяем статус
	switch paymentObj.Status {
	case payment.StatusApproved:
		// Платеж успешен - создаем подписки
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		// Платеж еще обрабатывается
		return h.sendPaymentPendingMessage(chatID, data)
	case payment.StatusRejected, payment.StatusCancelled:
		// Платеж отклонен или отменен
		return h.sendError(chatID, messages.PaymentRejected)
	default:
		return h.sendPaymentCheckError(chatID, data, messages.PaymentUnknownStatus)
	}
}

// sendPaymentPendingMessage отправляет сообщение о том, что платеж еще обрабатывается
func (h *Handler) sendPaymentPendingMessage(chatID int64, data *flows.BuySubFlowData) error {
	messageText := messages.PaymentPending

	checkButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCheckAgain, "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancelPurchase, "cancel_purchase")

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

// sendPaymentCheckError отправляет сообщение об ошибке проверки с возможностью повторить
func (h *Handler) sendPaymentCheckError(chatID int64, data *flows.BuySubFlowData, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonRetry, "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData(messages.ButtonCancelPurchase, "cancel_purchase")

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

// handleSuccessfulPayment обрабатывает успешный платеж и создает подписки
func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, data *flows.BuySubFlowData, paymentID int64) error {
	// Создаем подписку после успешной оплаты
	subReq := &subs.CreateSubscriptionRequest{
		UserID:    data.UserID,
		TariffID:  data.TariffID,
		PaymentID: &paymentID,
	}

	subscription, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription after payment", "error", err, "paymentID", paymentID)
		// Send reassuring message that the system will retry automatically
		errorText := messages.SubscriptionErrorCreatingWillRetry

		// Редактируем существующее сообщение, если MessageID есть
		if data.MessageID != nil {
			editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, errorText)
			editMsg.ReplyMarkup = nil // Убираем кнопки
			_, sendErr := h.bot.Send(editMsg)
			if sendErr != nil {
				h.logger.Error("Failed to edit message with retry info", "error", sendErr)
			}
		} else {
			// Fallback: отправляем новое сообщение
			msg := tgbotapi.NewMessage(chatID, errorText)
			_, sendErr := h.bot.Send(msg)
			if sendErr != nil {
				h.logger.Error("Failed to send retry message", "error", sendErr)
			}
		}
		// Clear state since payment is processed and worker will handle retry
		h.stateManager.Clear(chatID)
		return nil
	}

	// Отправляем инструкции по подключению
	err = h.SendConnectionInstructions(chatID, subscription, data.MessageID)
	if err != nil {
		return h.sendError(chatID, messages.SubscriptionErrorSendingInstructions)
	}

	// Очищаем состояние флоу
	h.stateManager.Clear(chatID)

	return nil
}

// TariffCallbackData - структура для данных тарифа из callback
type TariffCallbackData struct {
	ID           int64
	Price        float64
	Name         string
	DurationDays int
}

// parseTariffFromCallback парсит данные тарифа из callback data
func (h *Handler) parseTariffFromCallback(callbackData string) (*TariffCallbackData, error) {
	if !strings.HasPrefix(callbackData, "tariff:") {
		return nil, fmt.Errorf("invalid callback format")
	}

	// Формат: tariff:id:price:name:days
	parts := strings.Split(callbackData, ":")
	if len(parts) != 5 {
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

	name := parts[3]

	days, err := strconv.Atoi(parts[4])
	if err != nil {
		return nil, fmt.Errorf("invalid tariff duration: %w", err)
	}

	return &TariffCallbackData{
		ID:           id,
		Price:        price,
		Name:         name,
		DurationDays: days,
	}, nil
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

// SendConnectionInstructions отправляет инструкции по подключению после успешной оплаты
func (h *Handler) SendConnectionInstructions(chatID int64, subscription *subs.Subscription, messageID *int) error {
	wgData, err := subscription.GetWireGuardData()

	if err != nil || wgData == nil || wgData.ConfigFile == "" {
		messageText := messages.SubscriptionSuccessPaid + "\n\n" + messages.SubscriptionLinkNotReady
		keyboard := h.createConnectionKeyboard(nil)

		if messageID != nil {
			editMsg := tgbotapi.NewEditMessageText(chatID, *messageID, messageText)
			editMsg.ReplyMarkup = &keyboard
			editMsg.DisableWebPagePreview = true
			_, err := h.bot.Send(editMsg)
			return err
		}

		msg := tgbotapi.NewMessage(chatID, messageText)
		msg.ReplyMarkup = keyboard
		msg.DisableWebPagePreview = true
		_, err = h.bot.Send(msg)
		return err
	}

	successText := messages.SubscriptionSuccessPaid

	if messageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *messageID, successText)
		editMsg.DisableWebPagePreview = true
		_, _ = h.bot.Send(editMsg)
	}

	instructionsText := messages.SubscriptionInstructions + "\n\n" + messages.SubscriptionSupportNote

	qrBytes, err := base64.StdEncoding.DecodeString(wgData.QRCodeBase64)
	if err != nil {
		h.logger.Error("Failed to decode QR code", "error", err)
	} else {
		qrPhoto := tgbotapi.FileBytes{
			Name:  "wireguard_qr.png",
			Bytes: qrBytes,
		}

		photoMsg := tgbotapi.NewPhoto(chatID, qrPhoto)
		photoMsg.Caption = instructionsText
		_, err = h.bot.Send(photoMsg)
		if err != nil {
			h.logger.Error("Failed to send QR code photo", "error", err)
		}
	}

	configBytes := []byte(wgData.ConfigFile)
	configFile := tgbotapi.FileBytes{
		Name:  "wireguard.conf",
		Bytes: configBytes,
	}

	configID := h.configStore.Store(wgData.ConfigFile, wgData.QRCodeBase64)
	wgLink := fmt.Sprintf("%s/wg/connect?id=%s", h.webAppBaseURL, configID)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🔗 "+messages.ButtonOpenVPNPage, wgLink),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMySubscriptions, "my_subscriptions"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMainMenu, "cancel"),
		),
	)

	docMsg := tgbotapi.NewDocument(chatID, configFile)
	docMsg.Caption = messages.SubscriptionConfigFile
	docMsg.ReplyMarkup = keyboard
	_, err = h.bot.Send(docMsg)
	if err != nil {
		h.logger.Error("Failed to send config file", "error", err)
	}

	return nil
}

// createConnectionKeyboard создает упрощенную клавиатуру для сообщения с подключениями
func (h *Handler) createConnectionKeyboard(wgData *subs.WireGuardData) tgbotapi.InlineKeyboardMarkup {
	if wgData != nil && wgData.ConfigFile != "" {
		configID := h.configStore.Store(wgData.ConfigFile, wgData.QRCodeBase64)
		wgLink := fmt.Sprintf("%s/wg/connect?id=%s", h.webAppBaseURL, configID)

		return tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("🔗 Подключиться к VPN", wgLink),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMySubscriptions, "my_subscriptions"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMainMenu, "cancel"),
			),
		)
	}

	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMySubscriptions, "my_subscriptions"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(messages.ButtonMainMenu, "cancel"),
		),
	)
}

// createFreeSubscription создает бесплатную подписку без оплаты
func (h *Handler) createFreeSubscription(ctx context.Context, chatID int64, data *flows.BuySubFlowData) error {
	// Создаем подписку без платежа
	subReq := &subs.CreateSubscriptionRequest{
		UserID:    data.UserID,
		TariffID:  data.TariffID,
		PaymentID: nil, // Без платежа для бесплатного тарифа
	}

	subscription, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create free subscription", "error", err)
		return h.sendError(chatID, messages.SubscriptionErrorCreating)
	}

	// Отправляем инструкции по подключению
	err = h.SendConnectionInstructions(chatID, subscription, data.MessageID)
	if err != nil {
		return h.sendError(chatID, messages.SubscriptionErrorSendingInstructions)
	}

	// Очищаем состояние флоу
	h.stateManager.Clear(chatID)

	return nil
}
