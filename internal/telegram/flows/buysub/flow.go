package buysub

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
	tariffService       tariffService
	subscriptionService subscriptionService
	paymentService      paymentService
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	ss subscriptionService,
	ps paymentService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		subscriptionService: ss,
		paymentService:      ps,
		logger:              logger,
	}
}

// Start начинает flow покупки подписки
func (h *Handler) Start(userID, chatID int64) error {
	// Инициализируем данные флоу с внутренним ID пользователя
	flowData := &flows.BuySubFlowData{
		UserID: userID,
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

		msg := tgbotapi.NewMessage(chatID, "❌ К сожалению, активных тарифов сейчас нет")
		_, err = h.bot.Send(msg)
		return err
	}

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffs)

	msg := tgbotapi.NewMessage(chatID, "📅 Выберите тариф:")
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

// handleTariffSelection обработка выбора тарифа
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		// Проверяем есть ли активные тарифы, если нет - выходим из flow
		tariffs, err := h.tariffService.GetActiveTariffs(ctx)
		if err == nil && len(tariffs) == 0 {
			h.stateManager.Clear(chatID)
			return h.sendError(chatID, "❌ Активные тарифы отсутствуют")
		}
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

	// Получаем существующие данные флоу, чтобы сохранить UserID
	flowData, err := h.stateManager.GetBuySubData(chatID)
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
	h.stateManager.SetState(chatID, states.UserBuySubWaitPayment, flowData)

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
	data, err := h.stateManager.GetBuySubData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обрабатываем разные типы callback
	switch {
	case callbackData == "payment_completed":
		return h.handlePaymentCompleted(ctx, update, data)
	case callbackData == "cancel_purchase" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
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
		return h.sendError(chatID, "❌ Ошибка создания платежа")
	}

	// Проверяем что ссылка на оплату была создана
	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "❌ Ошибка генерации ссылки на оплату")
	}

	// Сохраняем данные платежа в флоу
	data.PaymentID = &paymentObj.ID
	data.PaymentURL = paymentObj.PaymentURL

	// Показываем сообщение с ссылкой на оплату
	paymentMsg := fmt.Sprintf(
		"💳 *Заказ создан!*\n\n"+
			"📋 Заказ #%d\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Перейдите по ссылке для оплаты.\n"+
			"После оплаты вернитесь сюда и нажмите «Оплатил».",
		paymentObj.ID, data.TariffName, data.TotalAmount)

	// Создаем ссылку для оплаты
	paymentButtonText := fmt.Sprintf("💳 Оплатить %.2f ₽", data.TotalAmount)
	paymentButton := tgbotapi.NewInlineKeyboardButtonURL(paymentButtonText, *paymentObj.PaymentURL)
	completeButton := tgbotapi.NewInlineKeyboardButtonData("✅ Оплатил", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

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

	// Сохраняем обновленное состояние с данными платежа
	h.stateManager.SetState(chatID, states.UserBuySubWaitPayment, data)

	return nil
}

// handleCancel обрабатывает отмену любого действия и возвращает в главное меню
func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Возвращаемся в главное меню")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Отправляем главное меню
	return h.sendMainMenu(chatID)
}

// sendMainMenu отправляет главное меню
func (h *Handler) sendMainMenu(chatID int64) error {
	text := "Доступные команды:\n" +
		"/start — Начать работу\n" +
		"/buy — Купить ключ доступа"

	msg := tgbotapi.NewMessage(chatID, text)
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
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// formatDuration форматирует длительность в удобный формат (дни/месяцы/годы)
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
func (h *Handler) handlePaymentCompleted(ctx context.Context, update *tgbotapi.Update, data *flows.BuySubFlowData) error {
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
		// Платеж успешен - создаем подписки
		return h.handleSuccessfulPayment(ctx, chatID, data, *data.PaymentID)
	case payment.StatusPending:
		// Платеж еще обрабатывается
		return h.sendPaymentPendingMessage(chatID, data)
	case payment.StatusRejected, payment.StatusCancelled:
		// Платеж отклонен или отменен
		return h.sendError(chatID, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendPaymentCheckError(chatID, data, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// sendPaymentPendingMessage отправляет сообщение о том, что платеж еще обрабатывается
func (h *Handler) sendPaymentPendingMessage(chatID int64, data *flows.BuySubFlowData) error {
	msg := tgbotapi.NewMessage(chatID,
		"⏳ Платеж еще обрабатывается.\n"+
			"Пожалуйста, подождите немного и попробуйте еще раз.")

	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
	return err
}

// sendPaymentCheckError отправляет сообщение об ошибке проверки с возможностью повторить
func (h *Handler) sendPaymentCheckError(chatID int64, data *flows.BuySubFlowData, errorMsg string) error {
	msg := tgbotapi.NewMessage(chatID, errorMsg)

	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(retryButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	msg.ReplyMarkup = keyboard
	_, err := h.bot.Send(msg)
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
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем инструкции по подключению
	err = h.SendConnectionInstructions(chatID, subscription)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка отправки инструкций")
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
	if update.CallbackQuery != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}

// SendConnectionInstructions отправляет инструкции по подключению после успешной оплаты
func (h *Handler) SendConnectionInstructions(chatID int64, subscription *subs.Subscription) error {
	messageText := "✅ *Оплата прошла успешно\\!*\n\n" +
		"🎉 Ваша подписка активирована\\!\n\n" +
		"🔗 *Ссылка подключения:*\n"

	if subscription.MarzbanLink != "" {
		messageText += fmt.Sprintf("`%s`\n\n", subscription.MarzbanLink)
	} else {
		messageText += "❌ Ссылка подключения не готова\n\n"
	}

	messageText += "📋 *Инструкция по подключению:*\n\n"
	messageText += "📱 *1\\. Скачайте приложение v2RayTun:*\n"
	messageText += "• Android: [Google Play](https://play.google.com/store/apps/details?id=com.v2raytun.android)\n"
	messageText += "• iOS: [App Store](https://apps.apple.com/us/app/v2raytun/id6476628951)\n\n"
	messageText += "📋 *2\\. Настройте подключение:*\n"
	messageText += "• Скопируйте ссылку подключения выше\n"
	messageText += "• Откройте v2RayTun\n"
	messageText += "• Добавьте конфигурацию через \\\"Импорт из буфера\\\"\n\n"
	messageText += "⚠️ *Если v2RayTun не работает, используйте Happ:*\n"
	messageText += "• Android: [Google Play](https://play.google.com/store/apps/details?id=com.happproxy)\n"
	messageText += "• iOS: [App Store](https://apps.apple.com/us/app/happ\\-proxy\\-utility/id6504287215)\n\n"
	messageText += "❓ Проблемы с подключением? Обратитесь в поддержку: /support"

	keyboard := h.createConnectionKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "MarkdownV2"
	msg.ReplyMarkup = keyboard
	msg.DisableWebPagePreview = true

	_, err := h.bot.Send(msg)
	return err
}

// createConnectionKeyboard создает упрощенную клавиатуру для сообщения с подключениями
func (h *Handler) createConnectionKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Мои подписки", "my_subscriptions"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Главное меню", "cancel"),
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
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем инструкции по подключению
	err = h.SendConnectionInstructions(chatID, subscription)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка отправки инструкций")
	}

	// Очищаем состояние флоу
	h.stateManager.Clear(chatID)

	return nil
}
