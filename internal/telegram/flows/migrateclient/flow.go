package migrateclient

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
	"kurut-bot/internal/stories/servers"
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
	serverService       serverService
	subscriptionService subscriptionService
	paymentService      paymentService
	orderService        orderService
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	ss serverService,
	subSvc subscriptionService,
	ps paymentService,
	os orderService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		serverService:       ss,
		subscriptionService: subSvc,
		paymentService:      ps,
		orderService:        os,
		logger:              logger,
	}
}

// Start начинает flow миграции клиента
func (h *Handler) Start(userID, assistantTelegramID, chatID int64) error {
	// Инициализируем данные флоу
	flowData := &flows.MigrateClientFlowData{
		AdminUserID:         userID,
		AssistantTelegramID: assistantTelegramID,
	}
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitName, flowData)

	msg := tgbotapi.NewMessage(chatID, "📱 Введите номер WhatsApp клиента для миграции (например: +996555123456):")
	_, err := h.bot.Send(msg)
	return err
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminMigrateClientWaitName:
		return h.handleWhatsAppInput(ctx, update)
	case states.AdminMigrateClientWaitServer:
		return h.handleServerSelection(ctx, update)
	case states.AdminMigrateClientWaitTariff:
		return h.handleTariffSelection(ctx, update)
	case states.AdminMigrateClientWaitPayment:
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

	// Сначала нормализуем, потом валидируем
	whatsapp = normalizePhone(whatsapp)

	if !isValidPhoneNumber(whatsapp) {
		return h.sendError(chatID, "❌ Неверный формат номера. Введите номер в формате +996555123456")
	}

	// Получаем данные флоу
	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Сохраняем WhatsApp номер
	flowData.ClientWhatsApp = whatsapp

	// Переводим в состояние выбора сервера
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitServer, flowData)

	// Показываем список серверов
	return h.showServers(ctx, chatID)
}

// showServers показывает список серверов для выбора
func (h *Handler) showServers(ctx context.Context, chatID int64) error {
	// Получаем активные серверы (не архивированные)
	archivedFalse := false
	serversList, err := h.serverService.ListServers(ctx, servers.ListCriteria{
		Archived: &archivedFalse,
	})
	if err != nil {
		h.logger.Error("Failed to list servers", "error", err)
		return h.sendError(chatID, "❌ Ошибка загрузки серверов")
	}

	if len(serversList) == 0 {
		h.stateManager.Clear(chatID)
		return h.sendError(chatID, "❌ Нет активных серверов")
	}

	// Создаем клавиатуру с серверами
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range serversList {
		text := fmt.Sprintf("🖥 %s", s.Name)
		callbackData := fmt.Sprintf("mig_srv:%d:%s", s.ID, s.Name)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Добавляем кнопку отмены
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "mig_cancel"),
	})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	flowData, _ := h.stateManager.GetMigrateClientData(chatID)
	text := fmt.Sprintf("🖥 Выберите сервер, на котором находится клиент:\n\n📱 Клиент: `%s`", flowData.ClientWhatsApp)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	if flowData != nil {
		flowData.MessageID = &sentMsg.MessageID
		h.stateManager.SetState(chatID, states.AdminMigrateClientWaitServer, flowData)
	}

	return nil
}

// handleServerSelection обработка выбора сервера
func (h *Handler) handleServerSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, выберите сервер из списка")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Проверяем на отмену
	if callbackData == "mig_cancel" {
		return h.handleCancel(update)
	}

	// Парсим данные сервера: mig_srv:123:ServerName
	if !strings.HasPrefix(callbackData, "mig_srv:") {
		return h.sendError(chatID, "Неверные данные сервера")
	}

	parts := strings.SplitN(callbackData, ":", 3)
	if len(parts) != 3 {
		return h.sendError(chatID, "Неверный формат данных сервера")
	}

	serverID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID сервера")
	}
	serverName := parts[2]

	// Получаем существующие данные флоу
	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные о сервере
	flowData.ServerID = serverID
	flowData.ServerName = serverName

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Сервер выбран")
	_, _ = h.bot.Request(callbackConfig)

	// Переводим в состояние выбора тарифа
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitTariff, flowData)

	// Показываем список тарифов
	return h.showTariffs(ctx, chatID)
}

// showTariffs показывает список тарифов
func (h *Handler) showTariffs(ctx context.Context, chatID int64) error {
	// Получаем активные тарифы
	tariffsList, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		h.logger.Error("Failed to get active tariffs", "error", err)
		return h.sendError(chatID, "❌ Ошибка загрузки тарифов")
	}

	if len(tariffsList) == 0 {
		h.stateManager.Clear(chatID)
		return h.sendError(chatID, "❌ Нет активных тарифов")
	}

	// Создаем клавиатуру с тарифами
	keyboard := h.createTariffsKeyboard(tariffsList)

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil || flowData == nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	text := fmt.Sprintf("📅 Выберите тариф:\n\n📱 Клиент: `%s`\n🖥 Сервер: %s",
		flowData.ClientWhatsApp, flowData.ServerName)

	// Редактируем существующее сообщение
	if flowData.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, text)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	sentMsg, err := h.bot.Send(msg)
	if err != nil {
		return err
	}

	flowData.MessageID = &sentMsg.MessageID
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitTariff, flowData)

	return nil
}

// handleTariffSelection обработка выбора тарифа
func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, выберите тариф из меню")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Проверяем на отмену
	if callbackData == "mig_cancel" {
		return h.handleCancel(update)
	}

	// Парсим данные тарифа
	tariffData, err := h.parseTariffFromCallback(callbackData)
	if err != nil {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	// Получаем существующие данные флоу
	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные о тарифе
	flowData.TariffID = tariffData.ID
	flowData.TariffName = tariffData.Name
	flowData.Price = tariffData.Price

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём заказ...")
	_, _ = h.bot.Request(callbackConfig)

	// Если тариф бесплатный - сразу создаем подписку без оплаты
	if tariffData.Price == 0 {
		return h.createMigratedSubscription(ctx, chatID, flowData)
	}

	// Переводим в состояние ожидания оплаты
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitPayment, flowData)

	// Создаём платёж и показываем ссылку на оплату
	return h.createPaymentAndShow(ctx, chatID, flowData)
}

// createMigratedSubscription создает подписку для мигрированного клиента
func (h *Handler) createMigratedSubscription(ctx context.Context, chatID int64, data *flows.MigrateClientFlowData) error {
	// Создаем запрос на миграцию
	req := &subs.MigrateSubscriptionRequest{
		UserID:              data.AdminUserID,
		TariffID:            data.TariffID,
		ServerID:            data.ServerID,
		ClientWhatsApp:      data.ClientWhatsApp,
		CreatedByTelegramID: data.AssistantTelegramID,
	}

	result, err := h.subscriptionService.MigrateSubscription(ctx, req)
	if err != nil {
		h.logger.Error("Failed to migrate subscription", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Отправляем сообщение об успехе
	return h.sendSubscriptionCreated(chatID, result, data)
}

// sendSubscriptionCreated отправляет сообщение об успешном создании подписки
func (h *Handler) sendSubscriptionCreated(chatID int64, result *subs.CreateSubscriptionResult, data *flows.MigrateClientFlowData) error {
	// Формируем пароль если есть
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	messageText := fmt.Sprintf(
		"✅ *Клиент мигрирован успешно!*\n\n"+
			"📱 Клиент: `%s`\n"+
			"🖥 Сервер: %s\n"+
			"📅 Тариф: %s\n\n"+
			"🔑 User ID:\n`%s`\n"+
			"🔐 Пароль:%s",
		data.ClientWhatsApp,
		data.ServerName,
		data.TariffName,
		result.GeneratedUserID,
		passwordLine,
	)

	// Создаем кнопки
	whatsappLink := generateWhatsAppLink(data.ClientWhatsApp, "Ваша подписка VPN активирована!")

	var rows [][]tgbotapi.InlineKeyboardButton

	// Добавляем кнопку для открытия панели управления сервером
	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🖥 Открыть панель управления", *result.ServerUIURL),
		))
	}

	// Добавляем кнопку для написания клиенту
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

// handleCancel обрабатывает отмену
func (h *Handler) handleCancel(update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, _ = h.bot.Request(callbackConfig)

	// Отправляем главное меню
	return h.sendMainMenu(chatID)
}

// sendMainMenu отправляет главное меню
func (h *Handler) sendMainMenu(chatID int64) error {
	text := "📱 Доступные команды:\n" +
		"/create_sub — Создать подписку для клиента\n" +
		"/migrate_client — Мигрировать существующего клиента\n" +
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
		callbackData := fmt.Sprintf("mig_trf:%d:%.2f:%s:%d", t.ID, t.Price, t.Name, t.DurationDays)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	// Добавляем кнопку отмены
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "mig_cancel"),
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

// TariffCallbackData - структура для данных тарифа из callback
type TariffCallbackData struct {
	ID           int64
	Price        float64
	Name         string
	DurationDays int
}

// parseTariffFromCallback парсит данные тарифа из callback data
func (h *Handler) parseTariffFromCallback(callbackData string) (*TariffCallbackData, error) {
	if !strings.HasPrefix(callbackData, "mig_trf:") {
		return nil, fmt.Errorf("invalid callback format")
	}

	// Формат: mig_trf:id:price:name:days
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

// normalizePhone очищает номер телефона, оставляя только цифры
func normalizePhone(phone string) string {
	var result strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// isValidPhoneNumber проверяет что нормализованный номер телефона валиден
func isValidPhoneNumber(normalizedPhone string) bool {
	match, _ := regexp.MatchString(`^[0-9]{10,15}$`, normalizedPhone)
	return match
}

// generateWhatsAppLink генерирует ссылку на WhatsApp с предзаполненным сообщением
func generateWhatsAppLink(phone string, message string) string {
	// Убираем + из начала номера для WhatsApp API
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")

	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}

// createPaymentAndShow создает платеж и показывает ссылку на оплату
func (h *Handler) createPaymentAndShow(ctx context.Context, chatID int64, data *flows.MigrateClientFlowData) error {
	// Создаем платеж
	paymentEntity := payment.Payment{
		UserID: data.AdminUserID,
		Amount: data.Price,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		h.logger.Error("Failed to create payment",
			"error", err,
			"user_id", data.AdminUserID,
			"amount", data.Price)
		return h.sendError(chatID, "Ошибка создания платежа. Попробуйте позже или обратитесь к администратору.")
	}

	// Mock mode: платёж уже approved, сразу создаём подписку
	if paymentObj.PaymentURL == nil && paymentObj.Status == payment.StatusApproved {
		return h.createMigratedSubscription(ctx, chatID, data)
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
		TotalAmount:         data.Price,
		ServerID:            &data.ServerID,
		ServerName:          &data.ServerName,
	}

	createdOrder, err := h.orderService.CreatePendingOrder(ctx, pendingOrder)
	if err != nil {
		h.logger.Error("Failed to create pending order", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания заказа")
	}

	// Показываем сообщение с ссылкой на оплату
	paymentMsg := fmt.Sprintf(
		"💳 Заказ на миграцию создан!\n\n"+
			"📱 Клиент: %s\n"+
			"🖥 Сервер: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Ссылка на оплату: [link](%s)\n\n",
		data.ClientWhatsApp, data.ServerName, data.TariffName, data.Price, *paymentObj.PaymentURL)

	// Создаем кнопки с orderID для независимой работы каждого заказа
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", fmt.Sprintf("migpay_check:%d", createdOrder.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("🔗 Обновить ссылку", fmt.Sprintf("migpay_refresh:%d", createdOrder.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("migpay_cancel:%d", createdOrder.ID))

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

	// Очищаем состояние, чтобы админ мог начать новый флоу
	// Кнопки работают независимо через orderID
	h.stateManager.Clear(chatID)

	return nil
}

// handlePaymentConfirmation обработка подтверждения оплаты (для state-based flow)
func (h *Handler) handlePaymentConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Обрабатываем отмену
	if callbackData == "mig_cancel" {
		return h.handleCancel(update)
	}

	return h.sendError(chatID, "Используйте кнопки для проверки оплаты")
}

// HandleMigratePaymentCallback обрабатывает callbacks от кнопок оплаты миграции (migpay_check, migpay_refresh, migpay_cancel)
// Эти callbacks работают независимо от состояния пользователя через orderID
func (h *Handler) HandleMigratePaymentCallback(update *tgbotapi.Update) error {
	ctx := context.Background()

	if update.CallbackQuery == nil {
		return nil
	}

	callbackData := update.CallbackQuery.Data
	chatID := update.CallbackQuery.Message.Chat.ID

	// Парсим callback: migpay_check:123 → action="check", orderID=123
	parts := strings.Split(callbackData, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, "❌ Неверный формат callback")
	}

	action := strings.TrimPrefix(parts[0], "migpay_")
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
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Заказ не найден или уже обработан")
		_, _ = h.bot.Request(callbackConfig)
		return nil
	}

	switch action {
	case "check":
		return h.handleMigratePaymentCheck(ctx, update, order)
	case "refresh":
		return h.handleMigratePaymentRefresh(ctx, update, order)
	case "cancel":
		return h.handleMigratePaymentCancel(ctx, update, order)
	default:
		return h.sendError(chatID, "❌ Неизвестное действие")
	}
}

// handleMigratePaymentCheck проверяет статус платежа и создает подписку если оплачено
func (h *Handler) handleMigratePaymentCheck(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Проверяем платеж...")
	_, _ = h.bot.Request(callbackConfig)

	// Проверяем статус платежа
	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, order.PaymentID)
	if err != nil {
		h.logger.Error("Failed to check payment status", "error", err, "paymentID", order.PaymentID)
		return h.sendMigratePaymentError(chatID, order, "❌ Ошибка проверки платежа. Попробуйте еще раз.")
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		// Платеж успешен - создаем подписку
		return h.handleSuccessfulMigratePayment(ctx, chatID, order)
	case payment.StatusPending:
		// Платеж еще обрабатывается
		alertConfig := tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "⏳ Платеж еще обрабатывается.\nПожалуйста, подождите и попробуйте еще раз.")
		_, _ = h.bot.Request(alertConfig)
		return nil
	case payment.StatusRejected, payment.StatusCancelled:
		return h.sendMigratePaymentError(chatID, order, "❌ Платеж был отклонен или отменен")
	default:
		return h.sendMigratePaymentError(chatID, order, "❌ Неизвестный статус платежа. Попробуйте еще раз.")
	}
}

// handleSuccessfulMigratePayment создает подписку после успешной оплаты
func (h *Handler) handleSuccessfulMigratePayment(ctx context.Context, chatID int64, order *orders.PendingOrder) error {
	// Проверяем что ServerID указан
	if order.ServerID == nil {
		return h.sendError(chatID, "❌ Ошибка: сервер не указан")
	}

	// Создаем подписку
	req := &subs.MigrateSubscriptionRequest{
		UserID:              order.AdminUserID,
		TariffID:            order.TariffID,
		ServerID:            *order.ServerID,
		ClientWhatsApp:      order.ClientWhatsApp,
		CreatedByTelegramID: order.AssistantTelegramID,
	}

	result, err := h.subscriptionService.MigrateSubscription(ctx, req)
	if err != nil {
		h.logger.Error("Failed to create migrated subscription after payment", "error", err, "paymentID", order.PaymentID)
		return h.sendError(chatID, "❌ Ошибка создания подписки")
	}

	// Формируем данные для отображения
	serverName := ""
	if order.ServerName != nil {
		serverName = *order.ServerName
	}

	// Отправляем сообщение об успехе
	if err := h.sendMigrateSubscriptionCreatedForOrder(chatID, result, order, serverName); err != nil {
		return err
	}

	// Удаляем pending order
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err, "orderID", order.ID)
	}

	return nil
}

// handleMigratePaymentRefresh создает новый платеж и обновляет сообщение
func (h *Handler) handleMigratePaymentRefresh(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
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
		h.logger.Error("Failed to create payment for refresh", "error", err)
		return h.sendError(chatID, "Ошибка создания платежа. Попробуйте позже.")
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "Ошибка генерации ссылки на оплату")
	}

	// Обновляем paymentID в заказе
	if err := h.orderService.UpdatePaymentID(ctx, order.ID, paymentObj.ID); err != nil {
		h.logger.Error("Failed to update payment ID", "error", err, "orderID", order.ID)
	}

	serverName := ""
	if order.ServerName != nil {
		serverName = *order.ServerName
	}

	// Формируем обновленное сообщение
	paymentMsg := fmt.Sprintf(
		"💳 *Заказ на миграцию*\n\n"+
			"📱 Клиент: %s\n"+
			"🖥 Сервер: %s\n"+
			"📅 Тариф: %s\n"+
			"💰 Сумма: %.2f ₽\n\n"+
			"🔗 Ссылка на оплату: [link](%s)\n\n"+
			"После оплаты нажмите «Проверить оплату».",
		order.ClientWhatsApp, serverName, order.TariffName, order.TotalAmount, *paymentObj.PaymentURL)

	// Создаем кнопки
	checkButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Проверить оплату", fmt.Sprintf("migpay_check:%d", order.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("🔗 Обновить ссылку", fmt.Sprintf("migpay_refresh:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("migpay_cancel:%d", order.ID))

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

// handleMigratePaymentCancel отменяет заказ
func (h *Handler) handleMigratePaymentCancel(ctx context.Context, update *tgbotapi.Update, order *orders.PendingOrder) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, _ = h.bot.Request(callbackConfig)

	// Удаляем pending order
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err, "orderID", order.ID)
	}

	serverName := ""
	if order.ServerName != nil {
		serverName = *order.ServerName
	}

	// Редактируем сообщение
	if order.MessageID != nil {
		cancelledMsg := fmt.Sprintf(
			"❌ *Заказ отменен*\n\n"+
				"📱 Клиент: %s\n"+
				"🖥 Сервер: %s\n"+
				"📅 Тариф: %s",
			order.ClientWhatsApp, serverName, order.TariffName)

		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, cancelledMsg)
		editMsg.ParseMode = "Markdown"
		_, _ = h.bot.Send(editMsg)
	}

	return nil
}

// sendMigratePaymentError отправляет сообщение об ошибке проверки
func (h *Handler) sendMigratePaymentError(chatID int64, order *orders.PendingOrder, errorMsg string) error {
	retryButton := tgbotapi.NewInlineKeyboardButtonData("🔄 Попробовать еще раз", fmt.Sprintf("migpay_check:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("migpay_cancel:%d", order.ID))

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

// sendMigrateSubscriptionCreatedForOrder отправляет сообщение об успешном создании подписки
func (h *Handler) sendMigrateSubscriptionCreatedForOrder(chatID int64, result *subs.CreateSubscriptionResult, order *orders.PendingOrder, serverName string) error {
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	messageText := fmt.Sprintf(
		"✅ *Подписка создана успешно!*\n\n"+
			"📱 Клиент: `%s`\n"+
			"🖥 Сервер: %s\n"+
			"📅 Тариф: %s\n\n"+
			"🔑 User ID:\n`%s`\n"+
			"🔐 Пароль:%s",
		order.ClientWhatsApp,
		serverName,
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
