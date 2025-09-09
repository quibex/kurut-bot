package buysub

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

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
	h.stateManager.SetBuySubState(chatID, states.UserBuySubWaitTariff, flowData)

	// Показываем тарифы
	return h.showTariffs(chatID)
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.UserBuySubWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.UserBuySubWaitQuantity:
		return h.handleQuantityInput(ctx, update)
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
		msg := tgbotapi.NewMessage(chatID, "❌ К сожалению, активных тарифов сейчас нет")
		_, err = h.bot.Send(msg)
		return err
	}

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffs)

	msg := tgbotapi.NewMessage(chatID, "📱 Выберите тариф:")
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

// handleTariffSelection обработка выбора тарифа
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(update.Message.Chat.ID, "Пожалуйста, выберите тариф из меню")
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

	// Сохраняем данные о тарифе в флоу
	flowData := &flows.BuySubFlowData{
		TariffID:   tariffData.ID,
		TariffName: tariffData.Name,
		Price:      tariffData.Price,
	}

	// Переводим в состояние ввода количества
	h.stateManager.SetBuySubState(chatID, states.UserBuySubWaitQuantity, flowData)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Тариф выбран")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Показываем форму ввода количества
	return h.showQuantityInput(chatID, tariffData.Name, tariffData.Price)
}

func (h *Handler) showQuantityInput(chatID int64, tariffName string, price float64) error {
	messageText := fmt.Sprintf(
		"📱 Тариф: *%s*\n"+
			"💰 Цена: %.2f ₽ за 1 подписку\n\n"+
			"🔢 Выберите количество подписок (от 1 до 100):",
		tariffName, price)

	keyboard := h.createQuantityKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	_, err := h.bot.Send(msg)
	return err
}

// createQuantityKeyboard создает клавиатуру для выбора количества
func (h *Handler) createQuantityKeyboard() tgbotapi.InlineKeyboardMarkup {
	// Кнопки с цифрами 1-5
	var row []tgbotapi.InlineKeyboardButton
	for i := 1; i <= 5; i++ {
		button := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d", i), fmt.Sprintf("qty:%d", i))
		row = append(row, button)
	}

	// Добавляем кнопку отмены
	cancelRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	}

	return tgbotapi.NewInlineKeyboardMarkup(row, cancelRow)
}

// handleQuantityInput обработка выбора количества подписок
func (h *Handler) handleQuantityInput(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора количества")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Проверяем на отмену
	if callbackData == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Парсим количество из callback data
	if !strings.HasPrefix(callbackData, "qty:") {
		return h.sendError(chatID, "Неверный формат данных")
	}

	quantityStr := strings.TrimPrefix(callbackData, "qty:")
	quantity, err := strconv.Atoi(quantityStr)
	if err != nil || quantity < 1 || quantity > 100 {
		return h.sendError(chatID, "Неверное количество подписок")
	}

	// Получаем данные флоу
	data, err := h.stateManager.GetBuySubData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные
	data.QuantitySub = quantity
	data.TotalAmount = data.Price * float64(quantity)

	// Переводим в состояние подтверждения
	h.stateManager.SetBuySubState(chatID, states.UserBuySubWaitPayment, data)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, fmt.Sprintf("Выбрано: %d подписок", quantity))
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Показываем подтверждение оплаты
	return h.showPaymentConfirmation(chatID, data)
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
	case callbackData == "proceed_payment":
		return h.createPaymentAndFinish(ctx, update, data)
	case callbackData == "cancel_purchase" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
}

// showPaymentConfirmation показывает подтверждение оплаты
func (h *Handler) showPaymentConfirmation(chatID int64, data *flows.BuySubFlowData) error {
	messageText := fmt.Sprintf(
		"📋 *Детали заказа:*\n\n"+
			"📱 Тариф: *%s*\n"+
			"💰 Цена за 1 шт: %.2f ₽\n"+
			"🔢 Количество: %d\n"+
			"💳 **Итого к оплате: %.2f ₽**\n\n"+
			"💳 Нажмите кнопку ниже для перехода к оплате:",
		data.TariffName, data.Price, data.QuantitySub, data.TotalAmount)

	keyboard := h.createPaymentKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	_, err := h.bot.Send(msg)
	return err
}

// createPaymentKeyboard создает клавиатуру для подтверждения оплаты
func (h *Handler) createPaymentKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💳 Перейти к оплате", "proceed_payment"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase"),
		),
	)
}

// createPaymentAndFinish создает платеж и завершает флоу
func (h *Handler) createPaymentAndFinish(ctx context.Context, update *tgbotapi.Update, data *flows.BuySubFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Создаем платеж - paymentService сам генерирует ссылку через cardlink
	// Используем внутренний ID пользователя из данных флоу
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

	// TODO: remove this
	// В реальном приложении webhook будет вызывать эту функцию автоматически
	// Для мока - симулируем успешную оплату через 5 секунд
	go func() {
		time.Sleep(5 * time.Second)
		h.logger.Info("Simulating payment success")
		h.handlePaymentWebhookSuccess(context.Background(), data.UserID, chatID, paymentObj.ID, data.TariffID, data.QuantitySub)
	}()

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Переходим к оплате...")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	// Показываем сообщение с ссылкой на оплату
	paymentMsg := fmt.Sprintf(
		"💳 *Заказ создан!*\n\n"+
			"📋 Заказ #%d\n"+
			"📱 Тариф: %s\n"+
			"🔢 Количество: %d\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Нажмите кнопку ниже для перехода к оплате.\n"+
			"После успешной оплаты подписки будут активированы автоматически.",
		paymentObj.ID, data.TariffName, data.QuantitySub, data.TotalAmount)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("💳 Оплатить", *paymentObj.PaymentURL),
		),
	)

	msg := tgbotapi.NewMessage(chatID, paymentMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	if err != nil {
		return err
	}

	// Очищаем состояние пользователя (используем внутренний ID)
	h.stateManager.Clear(data.UserID)

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
		"/buy — Купить подписку VPN"

	msg := tgbotapi.NewMessage(chatID, text)
	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) createTariffsKeyboard(tariffs []*tariffs.Tariff) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffs {
		text := fmt.Sprintf("📱 %s - %.2f ₽ (%d дней)", t.Name, t.Price, t.DurationDays)
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

// handlePaymentWebhookSuccess обрабатывает webhook от cardlink о successful платеже
func (h *Handler) handlePaymentWebhookSuccess(ctx context.Context, userID, chatID, paymentID, tariffID int64, quantity int) {
	// Обновляем статус платежа
	cardlinkTxID := fmt.Sprintf("tx_%d_%d", paymentID, time.Now().Unix())
	err := h.paymentService.ProcessPaymentSuccess(ctx, paymentID, cardlinkTxID)
	if err != nil {
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка обработки платежа"))
		return
	}

	// Создаем подписки после успешной оплаты
	subReq := &subs.CreateSubscriptionsRequest{
		UserID:    userID,
		TariffID:  tariffID,
		Quantity:  quantity,
		PaymentID: &paymentID,
	}

	subscriptions, err := h.subscriptionService.CreateSubscriptions(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscriptions after payment", "error", err, "paymentID", paymentID)
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка создания подписок"))
		return
	}

	// Связываем платеж с подписками
	subscriptionIDs := make([]int64, len(subscriptions))
	for i, sub := range subscriptions {
		subscriptionIDs[i] = sub.ID
	}

	err = h.paymentService.LinkPaymentToSubscriptions(ctx, paymentID, subscriptionIDs)
	if err != nil {
		h.logger.Error("Failed to link payment to subscriptions", "error", err, "paymentID", paymentID)
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка связывания платежа с подписками"))
		return
	}

	// Отправляем инструкции по подключению
	err = h.SendConnectionInstructions(userID, chatID, subscriptions)
	if err != nil {
		_, _ = h.bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка отправки инструкций"))
		return
	}
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
func (h *Handler) SendConnectionInstructions(userID, chatID int64, subscriptions []subs.Subscription) error {
	if len(subscriptions) == 0 {
		return fmt.Errorf("no subscriptions provided")
	}

	// Создаем базовое сообщение
	messageText := fmt.Sprintf(
		"✅ *Оплата прошла успешно!*\n\n"+
			"🎉 Ваши подписки активированы:\n"+
			"🔢 Количество: *%d*\n\n",
		len(subscriptions))

	// Для каждой подписки выводим MarzbanLink в моношрифте
	for i, subscription := range subscriptions {
		messageText += fmt.Sprintf("🔗 *Подписка #%d (ID: %d):*\n", i+1, subscription.ID)

		if subscription.MarzbanLink != "" {
			messageText += fmt.Sprintf("```\n%s\n```\n\n", subscription.MarzbanLink)
		} else {
			messageText += "❌ Ссылка подключения не готова\n\n"
		}
	}

	messageText += "📋 *Инструкция:*\n"
	messageText += "1. Скопируйте ссылку подключения выше\n"
	messageText += "2. Откройте VPN приложение (V2RayNG, Shadowrocket и т.д.)\n"
	messageText += "3. Добавьте конфигурацию через \"Импорт из буфера\"\n\n"
	messageText += "❓ Если у вас возникли проблемы с подключением, обратитесь в поддержку: /support"

	// Создаем упрощенную клавиатуру
	keyboard := h.createConnectionKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

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
