package createsubforclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/orders"
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
	tariffService       tariffService
	subscriptionService subscriptionService
	paymentService      paymentService
	orderService        orderService
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	ss subscriptionService,
	ps paymentService,
	os orderService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		subscriptionService: ss,
		paymentService:      ps,
		orderService:        os,
		logger:              logger,
	}
}

// Start начинает flow создания подписки для клиента
func (h *Handler) Start(userID, assistantTelegramID, chatID int64) error {
	// Инициализируем данные флоу
	flowData := &flows.CreateSubForClientFlowData{
		AdminUserID:         userID,
		AssistantTelegramID: assistantTelegramID,
	}
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitClientName, flowData)

	msg := tgbotapi.NewMessage(chatID, "📱 Введите номер WhatsApp клиента (например: +996555123456):")
	_, err := h.bot.Send(msg)
	return err
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminCreateSubWaitClientName:
		return h.handleWhatsAppInput(ctx, update)
	case states.AdminCreateSubWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.AdminCreateSubWaitPayment:
		return h.handlePaymentConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown state: %s", state)
	}
}

// handleWhatsAppInput обрабатывает ввод номера WhatsApp
func (h *Handler) handleWhatsAppInput(ctx context.Context, update *tgbotapi.Update) error {
	if update.Message == nil || update.Message.Text == "" {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, введите номер WhatsApp текстом")
	}

	chatID := update.Message.Chat.ID
	whatsapp := strings.TrimSpace(update.Message.Text)

	// Валидация номера телефона (базовая)
	if !isValidPhoneNumber(whatsapp) {
		return h.sendError(chatID, "❌ Неверный формат номера. Введите номер в формате +996555123456")
	}

	// Получаем данные флоу
	flowData, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Сохраняем WhatsApp номер
	flowData.ClientWhatsApp = whatsapp

	// Переводим в состояние выбора тарифа
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)

	// Показываем тарифы
	return h.showTariffs(chatID)
}

// isValidPhoneNumber проверяет что строка похожа на номер телефона
func isValidPhoneNumber(phone string) bool {
	// Убираем пробелы и тире
	cleaned := strings.ReplaceAll(phone, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")

	// Проверяем что это похоже на номер телефона
	// Допускаем формат: +XXXXXXXXXXXX или 0XXXXXXXXX
	match, _ := regexp.MatchString(`^[\+]?[0-9]{10,15}$`, cleaned)
	return match
}

func (h *Handler) showTariffs(chatID int64) error {
	ctx := context.Background()
	tariffsList, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения тарифов: %w", err)
	}

	if len(tariffsList) == 0 {
		// Очищаем состояние пользователя
		h.stateManager.Clear(chatID)

		msg := tgbotapi.NewMessage(chatID, "❌ К сожалению, активных тарифов сейчас нет")
		_, err = h.bot.Send(msg)
		return err
	}

	// Получаем данные флоу
	flowData, _ := h.stateManager.GetCreateSubForClientData(chatID)

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffsList)

	msg := tgbotapi.NewMessage(chatID, "📅 Выберите тариф:")
	msg.ReplyMarkup = keyboard

	// Отправляем сообщение и сохраняем его ID
	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Сохраняем MessageID
	if flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)
	}

	return nil
}

// handleTariffSelection обработка выбора тарифа
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		return h.sendError(chatID, "Пожалуйста, выберите тариф из меню")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	// Проверяем на отмену
	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Парсим данные тарифа
	tariffData, err := h.parseTariffFromCallback(update.CallbackQuery.Data)
	if err != nil {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	// Получаем существующие данные флоу
	flowData, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные о тарифе
	flowData.TariffID = tariffData.ID
	flowData.TariffName = tariffData.Name
	flowData.Price = tariffData.Price
	flowData.TotalAmount = tariffData.Price

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём заказ...")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Если тариф бесплатный - сразу создаем подписку без оплаты
	if tariffData.Price == 0 {
		return h.createFreeSubscription(ctx, chatID, flowData)
	}

	// Переводим в состояние ожидания оплаты
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitPayment, flowData)

	// Сразу создаём платёж и показываем ссылку на оплату
	return h.createPaymentAndShow(ctx, chatID, flowData)
}

// handlePaymentConfirmation обработка подтверждения оплаты
func (h *Handler) handlePaymentConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Получаем данные флоу
	data, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обрабатываем разные типы callback
	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "refresh_payment_link":
		return h.handleRefreshPaymentLink(ctx, update, data)
	case callbackData == "cancel_purchase" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
}

// handleRefreshPaymentLink обрабатывает обновление ссылки на оплату
func (h *Handler) handleRefreshPaymentLink(ctx context.Context, update *tgbotapi.Update, data *flows.CreateSubForClientFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём новую ссылку...")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Создаем новый платеж и показываем новую ссылку
	return h.createPaymentAndShow(ctx, chatID, data)
}

// createPaymentAndShow создает платеж и сразу показывает ссылку на оплату
func (h *Handler) createPaymentAndShow(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData) error {
	// Создаем платеж
	paymentEntity := payment.Payment{
		UserID: data.AdminUserID,
		Amount: data.TotalAmount,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		h.logger.Error("Failed to create payment",
			"error", err,
			"user_id", data.AdminUserID,
			"amount", data.TotalAmount)
		return h.sendError(chatID, "Ошибка создания платежа. Попробуйте позже или обратитесь к администратору.")
	}

	// Проверяем что ссылка на оплату была создана
	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "❌ Ошибка генерации ссылки на оплату")
	}

	// Создаем pending order для хранения контекста заказа
	pendingOrder := orders.PendingOrder{
		PaymentID:           paymentObj.ID,
		AdminUserID:         data.AdminUserID,
		AssistantTelegramID: data.AssistantTelegramID,
		ChatID:              chatID,
		ClientWhatsApp:      data.ClientWhatsApp,
		TariffID:            data.TariffID,
		TariffName:          data.TariffName,
		TotalAmount:         data.TotalAmount,
	}

	createdOrder, err := h.orderService.CreatePendingOrder(ctx, pendingOrder)
	if err != nil {
		h.logger.Error("Failed to create pending order", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания заказа")
	}

	// Показываем сообщение с ссылкой на оплату
	paymentMsg := fmt.Sprintf(
		"💳 Заказ создан!\n\n"+
			"📱 Клиент: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Ссылка на оплату: [link](%s)\n\n",
		data.ClientWhatsApp, data.TariffName, data.TotalAmount, *paymentObj.PaymentURL)

	// Создаем кнопки с orderID для независимой работы каждого заказа
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", fmt.Sprintf("pay_check:%d", createdOrder.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("🔗 Обновить ссылку", fmt.Sprintf("pay_refresh:%d", createdOrder.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", createdOrder.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(refreshButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем существующее сообщение, если MessageID есть
	var messageID int
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		if err != nil {
			return err
		}
		messageID = *data.MessageID
	} else {
		// Отправляем новое сообщение
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		sentMsg, err := h.bot.Send(msg)
		if err != nil {
			return err
		}
		messageID = sentMsg.MessageID
	}

	// Сохраняем MessageID в pending order для последующего редактирования
	if err := h.orderService.UpdateMessageID(ctx, createdOrder.ID, messageID); err != nil {
		h.logger.Error("Failed to update message ID", "error", err, "orderID", createdOrder.ID)
	}

	// ВАЖНО: очищаем состояние, чтобы админ мог начать новый флоу
	// Теперь кнопки работают независимо через orderID
	h.stateManager.Clear(chatID)

	return nil
}

// handleCancel обрабатывает отмену любого действия и возвращает в главное меню
func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем главное меню
	return h.sendMainMenu(chatID)
}

// sendMainMenu отправляет главное меню
func (h *Handler) sendMainMenu(chatID int64) error {
	text := "📱 Доступные команды:\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/my_subs — Список подписок"

	msg := tgbotapi.NewMessage(chatID, text)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) createTariffsKeyboard(tariffList []*tariffs.Tariff) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffList {
		durationText := formatDuration(t.DurationDays)
		text := fmt.Sprintf("📅 %s - %.2f ₽ (%s)", t.Name, t.Price, durationText)
		callbackData := fmt.Sprintf("tariff:%d:%.2f:%s:%d", t.ID, t.Price, t.Name, t.DurationDays)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Добавляем кнопку отмены
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration форматирует длительность в удобный формат
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

// handlePaymentCompleted обрабатывает нажатие кнопки "Оплатил"
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.CreateSubForClientFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Проверяем платеж...")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Проверяем что paymentID есть
	if data.PaymentID == nil {
		return h.sendError(chatID, "❌ Ошибка: платеж не найден")
	}

	// Проверяем статус платежа через API
	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, *data.PaymentID)
	if err != nil {
		return h.sendPaymentCheckError(chatID, data, "❌ Ошибка проверки платежа. Попробуйте еще раз.")
	}

	// Проверяем статус
	switch paymentObj.Status {
	case payment.StatusApproved:
		// Платеж успешен - создаем подписку
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		// Платеж еще обрабатывается - показываем всплывающее уведомление
		alertConfig := tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "⏳ Платеж еще обрабатывается.\nПожалуйста, подождите и попробуйте еще раз.")
		_, _ = h.bot.Request(alertConfig)
		return nil
	case payment.StatusRejected, payment.StatusCancelled:
		// Платеж отклонен или отменен
		return h.sendError(chatID, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendPaymentCheckError(chatID, data, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// sendPaymentPendingMessage отправляет сообщение о том, что платеж еще обрабатывается
func (h *Handler) sendPaymentPendingMessage(chatID int64, data *flows.CreateSubForClientFlowData) error {
	messageText := "⏳ Платеж еще обрабатывается.\n" +
		"Пожалуйста, подождите немного и попробуйте еще раз."

	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

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
func (h *Handler) sendPaymentCheckError(chatID int64, data *flows.CreateSubForClientFlowData, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

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

// handleSuccessfulPayment обрабатывает успешный платеж и создает подписку
func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData, paymentID int64) error {
	// Создаем подписку после успешной оплаты
	subReq := &subs.CreateSubscriptionRequest{
		UserID:              data.AdminUserID,
		TariffID:            data.TariffID,
		PaymentID:           &paymentID,
		ClientWhatsApp:      data.ClientWhatsApp,
		CreatedByTelegramID: data.AssistantTelegramID,
	}

	result, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription after payment", "error", err, "paymentID", paymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем информацию о созданной подписке
	return h.sendSubscriptionCreated(chatID, result, data)
}

// sendSubscriptionCreated отправляет сообщение об успешном создании подписки
func (h *Handler) sendSubscriptionCreated(chatID int64, result *subs.CreateSubscriptionResult, data *flows.CreateSubForClientFlowData) error {
	// Формируем пароль если есть
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	messageText := fmt.Sprintf(
		"✅ *Подписка создана успешно!*\n\n"+
			"📱 Клиент: `%s`\n"+
			"📅 Тариф: %s\n\n"+
			"🔑 User ID:\n`%s`\n"+
			"🔐 Пароль:%s",
		data.ClientWhatsApp,
		data.TariffName,
		result.GeneratedUserID,
		passwordLine,
	)

	// Создаем кнопки
	whatsappLink := generateWhatsAppLink(data.ClientWhatsApp, "Ваша подписка VPN активирована! Сейчас отправлю инструкции по подключению.")

	var rows [][]tgbotapi.InlineKeyboardButton

	// Добавляем кнопку для открытия панели управления сервером
	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🖥 Открыть панель управления", *result.ServerUIURL),
		))
	}

	// Добавляем кнопку для открытия WhatsApp
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("💬 Написать клиенту", whatsappLink),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	// Редактируем существующее сообщение, если MessageID есть
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		if err != nil {
			// Fallback: отправляем новое сообщение
			msg := tgbotapi.NewMessage(chatID, messageText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = keyboard
			_, err = h.bot.Send(msg)
		}
		h.stateManager.Clear(chatID)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)

	// Очищаем состояние флоу
	h.stateManager.Clear(chatID)

	return err
}

// createFreeSubscription создает бесплатную подписку без оплаты
func (h *Handler) createFreeSubscription(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData) error {
	// Создаем подписку без платежа
	subReq := &subs.CreateSubscriptionRequest{
		UserID:              data.AdminUserID,
		TariffID:            data.TariffID,
		PaymentID:           nil,
		ClientWhatsApp:      data.ClientWhatsApp,
		CreatedByTelegramID: data.AssistantTelegramID,
	}

	result, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create free subscription", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем информацию о созданной подписке
	return h.sendSubscriptionCreated(chatID, result, data)
}

// generateWhatsAppLink генерирует ссылку на WhatsApp с предзаполненным сообщением
func generateWhatsAppLink(phone string, message string) string {
	// Убираем + из начала номера для WhatsApp API
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")

	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
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

// HandlePaymentCallback обрабатывает callbacks от кнопок оплаты (pay_check, pay_refresh, pay_cancel)
// Эти callbacks работают независимо от состояния пользователя через orderID
func (h *Handler) HandlePaymentCallback(update *tgbotapi.Update) error {
	ctx := context.Background()

	if update.CallbackQuery == nil {
		return nil
	}

	callbackData := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Chat.ID

	// Парсим callback: pay_check:123 → action="check", orderID=123
	parts := strings.Split(callbackData, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, "❌ Неверный формат callback")
	}

	action := strings.TrimPrefix(parts[0], "pay_")
	orderID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "❌ Неверный ID заказа")
	}

	// Загружаем заказ из БД
	order, err := h.orderService.GetPendingOrderByID(ctx, orderID)
	if err != nil {
		h.logger.Error("Failed to get pending order", "error", err, "orderID", orderID)
		return h.sendError(chatID, "❌ Ошибка получения заказа")
	}
	if order == nil {
		return h.sendCallbackError(update, chatID, "❌ Заказ не найден или уже обработан")
	}

	switch action {
	case "check":
		return h.handlePaymentCheckFromOrder(ctx, update, order)
	case "refresh":
		return h.handlePaymentRefreshFromOrder(ctx, update, order)
	case "cancel":
		return h.handlePaymentCancelFromOrder(ctx, update, order)
	default:
		return h.sendError(chatID, "❌ Неизвестное действие")
	}
}

// handlePaymentCheckFromOrder проверяет статус платежа и создает подписку если оплачено
func (h *Handler) handlePaymentCheckFromOrder(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Проверяем платеж...")
	_, _ = h.bot.Request(callbackConfig)

	// Проверяем статус платежа
	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, order.PaymentID)
	if err != nil {
		h.logger.Error("Failed to check payment status", "error", err, "paymentID", order.PaymentID)
		return h.sendPaymentCheckErrorForOrder(chatID, order, "❌ Ошибка проверки платежа. Попробуйте еще раз.")
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		// Платеж успешен - создаем подписку
		return h.handleSuccessfulPaymentFromOrder(ctx, chatID, order)
	case payment.StatusPending:
		// Платеж еще обрабатывается - показываем всплывающее уведомление
		alertConfig := tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "⏳ Платеж еще обрабатывается.\nПожалуйста, подождите и попробуйте еще раз.")
		_, _ = h.bot.Request(alertConfig)
		return nil
	case payment.StatusRejected, payment.StatusCancelled:
		// Платеж отклонен или отменен
		return h.sendPaymentCheckErrorForOrder(chatID, order, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendPaymentCheckErrorForOrder(chatID, order, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// handleSuccessfulPaymentFromOrder создает подписку после успешной оплаты
func (h *Handler) handleSuccessfulPaymentFromOrder(ctx context.Context, chatID int64, order *orders.PendingOrder) error {
	// Создаем подписку
	subReq := &subs.CreateSubscriptionRequest{
		UserID:              order.AdminUserID,
		TariffID:            order.TariffID,
		PaymentID:           &order.PaymentID,
		ClientWhatsApp:      order.ClientWhatsApp,
		CreatedByTelegramID: order.AssistantTelegramID,
	}

	result, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription after payment", "error", err, "paymentID", order.PaymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем сообщение об успехе
	if err := h.sendSubscriptionCreatedForOrder(chatID, result, order); err != nil {
		return err
	}

	// Удаляем pending order - он больше не нужен
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err, "orderID", order.ID)
	}

	return nil
}

// handlePaymentRefreshFromOrder создает новый платеж и обновляет сообщение
func (h *Handler) handlePaymentRefreshFromOrder(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём новую ссылку...")
	_, _ = h.bot.Request(callbackConfig)

	// Создаем новый платеж
	paymentEntity := payment.Payment{
		UserID: order.AdminUserID,
		Amount: order.TotalAmount,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		h.logger.Error("Failed to create payment for refresh",
			"error", err,
			"user_id", order.AdminUserID,
			"amount", order.TotalAmount)
		return h.sendError(chatID, "Ошибка создания платежа. Попробуйте позже или обратитесь к администратору.")
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "Ошибка генерации ссылки на оплату")
	}

	// Обновляем paymentID в заказе
	if err := h.orderService.UpdatePaymentID(ctx, order.ID, paymentObj.ID); err != nil {
		h.logger.Error("Failed to update payment ID", "error", err, "orderID", order.ID)
	}

	// Формируем обновленное сообщение
	paymentMsg := fmt.Sprintf(
		"💳 *Заказ создан!*\n\n"+
			"📱 Клиент: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Ссылка на оплату: [link](%s)\n\n"+
			"Отправьте эту ссылку клиенту.\n"+
			"После оплаты нажмите «Проверить оплату».",
		order.ClientWhatsApp, order.TariffName, order.TotalAmount, *paymentObj.PaymentURL)

	// Создаем кнопки
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", fmt.Sprintf("pay_check:%d", order.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("🔗 Обновить ссылку", fmt.Sprintf("pay_refresh:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", order.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(refreshButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	// Редактируем сообщение
	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		return err
	}

	// Fallback: отправляем новое сообщение
	msg := tgbotapi.NewMessage(chatID, paymentMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Обновляем messageID в заказе
	if err := h.orderService.UpdateMessageID(ctx, order.ID, sentMsg.MessageID); err != nil {
		h.logger.Error("Failed to update message ID", "error", err, "orderID", order.ID)
	}

	return nil
}

// handlePaymentCancelFromOrder отменяет заказ
func (h *Handler) handlePaymentCancelFromOrder(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, _ = h.bot.Request(callbackConfig)

	// Удаляем pending order
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err, "orderID", order.ID)
	}

	// Редактируем сообщение чтобы показать что заказ отменен
	if order.MessageID != nil {
		cancelledMsg := fmt.Sprintf(
			"❌ *Заказ отменен*\n\n"+
				"📱 Клиент: %s\n"+
				"📅 Тариф: %s",
			order.ClientWhatsApp, order.TariffName)

		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, cancelledMsg)
		editMsg.ParseMode = "Markdown"
		_, _ = h.bot.Send(editMsg)
	}

	return nil
}

// sendPaymentPendingMessageForOrder отправляет сообщение о том, что платеж обрабатывается
func (h *Handler) sendPaymentPendingMessageForOrder(chatID int64, order *orders.PendingOrder) error {
	messageText := "⏳ Платеж еще обрабатывается.\n" +
		"Пожалуйста, подождите немного и попробуйте еще раз."

	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить еще раз", fmt.Sprintf("pay_check:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", order.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, messageText)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendPaymentCheckErrorForOrder отправляет сообщение об ошибке проверки
func (h *Handler) sendPaymentCheckErrorForOrder(chatID int64, order *orders.PendingOrder, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", fmt.Sprintf("pay_check:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("pay_cancel:%d", order.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(retryButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, errorMsg)
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, errorMsg)
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendSubscriptionCreatedForOrder отправляет сообщение об успешном создании подписки
func (h *Handler) sendSubscriptionCreatedForOrder(chatID int64, result *subs.CreateSubscriptionResult, order *orders.PendingOrder) error {
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	messageText := fmt.Sprintf(
		"✅ *Подписка создана успешно!*\n\n"+
			"📱 Клиент: `%s`\n"+
			"📅 Тариф: %s\n\n"+
			"🔑 User ID:\n`%s`\n"+
			"🔐 Пароль:%s",
		order.ClientWhatsApp,
		order.TariffName,
		result.GeneratedUserID,
		passwordLine,
	)

	whatsappLink := generateWhatsAppLink(order.ClientWhatsApp, "Ваша подписка VPN активирована! Сейчас отправлю инструкции по подключению.")

	var rows [][]tgbotapi.InlineKeyboardButton

	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🖥 Открыть панель управления", *result.ServerUIURL),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("💬 Написать клиенту", whatsappLink),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		if err != nil {
			// Fallback
			msg := tgbotapi.NewMessage(chatID, messageText)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = keyboard
			_, err = h.bot.Send(msg)
		}
		return err
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendCallbackError отвечает на callback с ошибкой и отправляет сообщение
func (h *Handler) sendCallbackError(update *tgbotapi.Update, chatID int64, errorMsg string) error {
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, errorMsg)
	_, _ = h.bot.Request(callbackConfig)
	return nil
}
