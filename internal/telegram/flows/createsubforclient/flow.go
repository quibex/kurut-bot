package createsubforclient

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
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot                 botApi
	stateManager        stateManager
	tariffService       tariffService
	subscriptionService subscriptionService
	paymentService      paymentService
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
		configStore:         configStore,
		webAppBaseURL:       webAppBaseURL,
		logger:              logger,
	}
}

// Start начинает flow создания подписки для клиента
func (h *Handler) Start(adminUserID, chatID int64) error {
	// Инициализируем данные флоу с ID админа
	flowData := &flows.CreateSubForClientFlowData{
		AdminUserID: adminUserID,
	}
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitClientName, flowData)

	msg := tgbotapi.NewMessage(chatID, "👤 Введите имя клиента (например: Тимур87-45):")
	_, err := h.bot.Send(msg)
	return err
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminCreateSubWaitClientName:
		return h.handleClientNameInput(ctx, update)
	case states.AdminCreateSubWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.AdminCreateSubWaitPayment:
		return h.handlePaymentConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown state: %s", state)
	}
}

// handleClientNameInput обрабатывает ввод имени клиента
func (h *Handler) handleClientNameInput(ctx context.Context, update *tgbotapi.Update) error {
	if update.Message == nil || update.Message.Text == "" {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, введите имя клиента текстом")
	}

	chatID := update.Message.Chat.ID
	clientName := strings.TrimSpace(update.Message.Text)

	if clientName == "" {
		return h.sendError(chatID, "Имя клиента не может быть пустым")
	}

	// Получаем данные флоу
	flowData, err := h.stateManager.GetCreateSubForClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Сохраняем имя клиента
	flowData.ClientName = clientName

	// Переводим в состояние выбора тарифа
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitTariff, flowData)

	// Показываем тарифы
	return h.showTariffs(chatID)
}

func (h *Handler) showTariffs(chatID int64) error {
	ctx := context.Background()
	tariffs, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения тарифов: %w", err)
	}

	if len(tariffs) == 0 {
		// Очищаем состояние пользователя
		h.stateManager.Clear(chatID)

		msg := tgbotapi.NewMessage(chatID, "❌ К сожалению, активных тарифов сейчас нет")
		_, err = h.bot.Send(msg)
		return err
	}

	// Получаем данные флоу
	flowData, _ := h.stateManager.GetCreateSubForClientData(chatID)

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffs)

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
	case callbackData == "cancel_purchase" || callbackData == "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
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
			"👤 Клиент: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 *Ссылка на оплату:*\n"+
			"%s\n\n"+
			"Отправьте эту ссылку клиенту.\n"+
			"После оплаты нажмите «Проверить оплату».",
		paymentObj.ID, data.ClientName, data.TariffName, data.TotalAmount, *paymentObj.PaymentURL)

	// Создаем кнопки без кнопки оплаты
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", "payment_completed")
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_purchase")

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
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
		// Fallback: отправляем новое сообщение
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
		msg.ReplyMarkup = keyboard
		sentMsg, err := h.bot.Send(msg)
		if err != nil {
			return err
		}
		data.MessageID = &sentMsg.MessageID
	}

	// Сохраняем обновленное состояние с данными платежа
	h.stateManager.SetState(chatID, states.AdminCreateSubWaitPayment, data)

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
		"/create_sub — Создать подписку для клиента\n" +
		"/create_tariff — Создать тариф\n" +
		"/disable_tariff — Архивировать тариф\n" +
		"/enable_tariff — Восстановить тариф"

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
	// Создаем подписку после успешной оплаты с именем клиента
	clientName := data.ClientName
	subReq := &subs.CreateSubscriptionRequest{
		UserID:     data.AdminUserID,
		TariffID:   data.TariffID,
		PaymentID:  &paymentID,
		ClientName: &clientName,
	}

	subscription, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create subscription after payment", "error", err, "paymentID", paymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем ключ администратору
	err = h.SendConnectionKey(chatID, subscription, data.ClientName, data.MessageID)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка отправки ключа")
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

// SendConnectionKey отправляет ключ подключения администратору
func (h *Handler) SendConnectionKey(chatID int64, subscription *subs.Subscription, clientName string, messageID *int) error {
	wgData, err := subscription.GetWireGuardData()

	if err != nil || wgData == nil || wgData.ConfigFile == "" {
		messageText := fmt.Sprintf(
			"✅ *Подписка создана успешно!*\n\n"+
				"👤 Клиент: *%s*\n"+
				"🔢 ID подписки: *%d*\n\n"+
				"⚠️ Конфигурация WireGuard не готова",
			clientName, subscription.ID)

		if messageID != nil {
			editMsg := tgbotapi.NewEditMessageText(chatID, *messageID, messageText)
			_, err := h.bot.Send(editMsg)
			return err
		}

		msg := tgbotapi.NewMessage(chatID, messageText)
		_, err = h.bot.Send(msg)
		return err
	}

	successText := fmt.Sprintf(
		"✅ *Подписка создана успешно!*\n\n"+
			"👤 Клиент: *%s*\n"+
			"🔢 ID подписки: *%d*\n\n"+
			"📋 Отправьте QR-код и конфигурацию клиенту для подключения.",
		clientName, subscription.ID)

	if messageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *messageID, successText)
		_, _ = h.bot.Send(editMsg)
	} else {
		msg := tgbotapi.NewMessage(chatID, successText)
		_, _ = h.bot.Send(msg)
	}

	qrBytes, err := base64.StdEncoding.DecodeString(wgData.QRCodeBase64)
	if err != nil {
		h.logger.Error("Failed to decode QR code", "error", err)
	} else {
		qrPhoto := tgbotapi.FileBytes{
			Name:  "wireguard_qr.png",
			Bytes: qrBytes,
		}

		photoMsg := tgbotapi.NewPhoto(chatID, qrPhoto)
		photoMsg.Caption = fmt.Sprintf("QR-код для клиента: *%s*", clientName)
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
			tgbotapi.NewInlineKeyboardButtonURL("🔗 Открыть страницу подключения", wgLink),
		),
	)

	docMsg := tgbotapi.NewDocument(chatID, configFile)
	docMsg.Caption = fmt.Sprintf("Файл конфигурации для клиента: *%s*", clientName)
	docMsg.ReplyMarkup = keyboard
	_, err = h.bot.Send(docMsg)
	if err != nil {
		h.logger.Error("Failed to send config file", "error", err)
	}

	return nil
}

// createFreeSubscription создает бесплатную подписку без оплаты
func (h *Handler) createFreeSubscription(ctx context.Context, chatID int64, data *flows.CreateSubForClientFlowData) error {
	// Создаем подписку без платежа
	clientName := data.ClientName
	subReq := &subs.CreateSubscriptionRequest{
		UserID:     data.AdminUserID,
		TariffID:   data.TariffID,
		PaymentID:  nil,
		ClientName: &clientName,
	}

	subscription, err := h.subscriptionService.CreateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to create free subscription", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем ключ администратору
	err = h.SendConnectionKey(chatID, subscription, data.ClientName, data.MessageID)
	if err != nil {
		return h.sendError(chatID, "❌ Ошибка отправки ключа")
	}

	// Очищаем состояние флоу
	h.stateManager.Clear(chatID)

	return nil
}
