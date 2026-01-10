package migrateclient

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
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
	srvs serverService,
	ss subscriptionService,
	ps paymentService,
	os orderService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		serverService:       srvs,
		subscriptionService: ss,
		paymentService:      ps,
		orderService:        os,
		logger:              logger,
	}
}

// Start начинает flow миграции клиента
func (h *Handler) Start(userID, assistantTelegramID, chatID int64) error {
	flowData := &flows.MigrateClientFlowData{
		AdminUserID:         userID,
		AssistantTelegramID: assistantTelegramID,
	}
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitName, flowData)

	msg := tgbotapi.NewMessage(chatID, "Введите номер WhatsApp клиента (например: +996555123456):")
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

func (h *Handler) handleWhatsAppInput(ctx context.Context, update *tgbotapi.Update) error {
	if update.Message == nil || update.Message.Text == "" {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, введите номер WhatsApp текстом")
	}

	chatID := update.Message.Chat.ID
	whatsapp := strings.TrimSpace(update.Message.Text)

	if !isValidPhoneNumber(whatsapp) {
		return h.sendError(chatID, "Неверный формат номера. Введите номер в формате +996555123456")
	}

	// Очищаем номер от пробелов и дефисов
	whatsapp = normalizePhone(whatsapp)

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	flowData.ClientWhatsApp = whatsapp
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitServer, flowData)

	return h.showServers(ctx, chatID)
}

func (h *Handler) showServers(ctx context.Context, chatID int64) error {
	notArchived := false
	serversList, err := h.serverService.ListServers(ctx, servers.ListCriteria{
		Archived: &notArchived,
		Limit:    100,
	})
	if err != nil {
		return h.sendError(chatID, "Ошибка получения списка серверов")
	}

	if len(serversList) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, "Нет доступных серверов")
		_, err = h.bot.Send(msg)
		return err
	}

	flowData, _ := h.stateManager.GetMigrateClientData(chatID)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range serversList {
		text := fmt.Sprintf("%s (%d/%d)", s.Name, s.CurrentUsers, s.MaxUsers)
		callbackData := fmt.Sprintf("mig_srv:%d:%s", s.ID, s.Name)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(text, callbackData),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "cancel"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, "Выберите сервер, на котором уже есть клиент:")
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

func (h *Handler) handleServerSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		return h.sendError(chatID, "Выберите сервер из списка")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(update)
	}

	if !strings.HasPrefix(update.CallbackQuery.Data, "mig_srv:") {
		return h.sendError(chatID, "Неверные данные")
	}

	parts := strings.SplitN(update.CallbackQuery.Data, ":", 3)
	if len(parts) != 3 {
		return h.sendError(chatID, "Неверные данные сервера")
	}

	serverID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID сервера")
	}
	serverName := parts[2]

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	flowData.ServerID = serverID
	flowData.ServerName = serverName

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
	_, _ = h.bot.Request(callbackConfig)

	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitTariff, flowData)

	return h.showTariffs(ctx, chatID)
}

func (h *Handler) showTariffs(ctx context.Context, chatID int64) error {
	tariffsList, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения тарифов")
	}

	if len(tariffsList) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, "Нет активных тарифов")
		_, err = h.bot.Send(msg)
		return err
	}

	flowData, _ := h.stateManager.GetMigrateClientData(chatID)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, t := range tariffsList {
		durationText := formatDuration(t.DurationDays)
		text := fmt.Sprintf("%s - %.2f ₽ (%s)", t.Name, t.Price, durationText)
		callbackData := fmt.Sprintf("mig_trf:%d:%.2f:%s", t.ID, t.Price, t.Name)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(text, callbackData),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "cancel"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if flowData.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, "Выберите тариф:")
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
	} else {
		msg := tgbotapi.NewMessage(chatID, "Выберите тариф:")
		msg.ReplyMarkup = keyboard
		sentMsg, sendErr := h.bot.Send(msg)
		if sendErr == nil {
			flowData.MessageID = &sentMsg.MessageID
			h.stateManager.SetState(chatID, states.AdminMigrateClientWaitTariff, flowData)
		}
		err = sendErr
	}

	return err
}

func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := update.Message.Chat.ID
		return h.sendError(chatID, "Выберите тариф из списка")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(update)
	}

	if !strings.HasPrefix(update.CallbackQuery.Data, "mig_trf:") {
		return h.sendError(chatID, "Неверные данные")
	}

	// mig_trf:id:price:name
	parts := strings.SplitN(update.CallbackQuery.Data, ":", 4)
	if len(parts) != 4 {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	tariffID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID тарифа")
	}
	price, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return h.sendError(chatID, "Неверная цена")
	}
	tariffName := parts[3]

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	flowData.TariffID = tariffID
	flowData.TariffName = tariffName
	flowData.Price = price

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём заказ...")
	_, _ = h.bot.Request(callbackConfig)

	// Если бесплатный тариф - сразу создаём подписку
	if price == 0 {
		return h.createMigratedSubscription(ctx, chatID, flowData, nil)
	}

	// Переводим в состояние ожидания оплаты
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitPayment, flowData)

	// Создаём платёж
	return h.createPaymentAndShow(ctx, chatID, flowData)
}

func (h *Handler) createPaymentAndShow(ctx context.Context, chatID int64, data *flows.MigrateClientFlowData) error {
	paymentEntity := payment.Payment{
		UserID: data.AdminUserID,
		Amount: data.Price,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		h.logger.Error("Failed to create payment", "error", err)
		return h.sendError(chatID, "Ошибка создания платежа")
	}

	// Mock mode: платёж уже approved, сразу создаём подписку
	if paymentObj.PaymentURL == nil && paymentObj.Status == payment.StatusApproved {
		return h.createMigratedSubscription(ctx, chatID, data, &paymentObj.ID)
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "Ошибка генерации ссылки на оплату")
	}

	// Создаём pending order (с server_id - это миграция)
	pendingOrder := orders.PendingOrder{
		PaymentID:           paymentObj.ID,
		AdminUserID:         data.AdminUserID,
		AssistantTelegramID: data.AssistantTelegramID,
		ChatID:              chatID,
		ClientWhatsApp:      data.ClientWhatsApp,
		ServerID:            &data.ServerID,
		ServerName:          &data.ServerName,
		TariffID:            data.TariffID,
		TariffName:          data.TariffName,
		TotalAmount:         data.Price,
	}

	createdOrder, err := h.orderService.CreatePendingOrder(ctx, pendingOrder)
	if err != nil {
		h.logger.Error("Failed to create migrate pending order", "error", err)
		return h.sendError(chatID, "Ошибка создания заказа")
	}

	paymentMsg := fmt.Sprintf(
		"*Заказ на миграцию создан*\n\n"+
			"Клиент: %s\n"+
			"Сервер: %s\n"+
			"Тариф: %s\n"+
			"Сумма: %.2f ₽\n\n"+
			"Ссылка на оплату: [оплатить](%s)",
		data.ClientWhatsApp, data.ServerName, data.TariffName, data.Price, *paymentObj.PaymentURL)

	checkButton := tgbotapi.NewInlineKeyboardButtonData("Проверить оплату", fmt.Sprintf("migpay_check:%d", createdOrder.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("Обновить ссылку", fmt.Sprintf("migpay_refresh:%d", createdOrder.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("Отменить", fmt.Sprintf("migpay_cancel:%d", createdOrder.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(refreshButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	var messageID int
	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		if _, err = h.bot.Send(editMsg); err != nil {
			return err
		}
		messageID = *data.MessageID
	} else {
		msg := tgbotapi.NewMessage(chatID, paymentMsg)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		sentMsg, sendErr := h.bot.Send(msg)
		if sendErr != nil {
			return sendErr
		}
		messageID = sentMsg.MessageID
	}

	if err := h.orderService.UpdateMessageID(ctx, createdOrder.ID, messageID); err != nil {
		h.logger.Error("Failed to update message ID", "error", err)
	}

	// Очищаем состояние - кнопки работают через orderID
	h.stateManager.Clear(chatID)

	return nil
}

func (h *Handler) handlePaymentConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	// Этот метод больше не используется напрямую - оплата через callbacks
	return nil
}

// HandlePaymentCallback обрабатывает callbacks оплаты миграции
func (h *Handler) HandlePaymentCallback(ctx context.Context, query *tgbotapi.CallbackQuery) error {
	chatID := query.Message.Chat.ID
	data := query.Data

	parts := strings.Split(data, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, "Неверный формат")
	}

	action := strings.TrimPrefix(parts[0], "migpay_")
	orderID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID заказа")
	}

	order, err := h.orderService.GetPendingOrderByID(ctx, orderID)
	if err != nil {
		h.logger.Error("Failed to get pending order", "error", err)
		return h.sendError(chatID, "Ошибка получения заказа")
	}
	if order == nil {
		callback := tgbotapi.NewCallback(query.ID, "Заказ не найден или уже обработан")
		_, _ = h.bot.Request(callback)
		return nil
	}

	switch action {
	case "check":
		return h.handlePaymentCheck(ctx, query, order)
	case "refresh":
		return h.handlePaymentRefresh(ctx, query, order)
	case "cancel":
		return h.handlePaymentCancel(ctx, query, order)
	}

	return nil
}

func (h *Handler) handlePaymentCheck(ctx context.Context, query *tgbotapi.CallbackQuery, order *orders.PendingOrder) error {
	chatID := query.Message.Chat.ID

	callback := tgbotapi.NewCallback(query.ID, "Проверяем...")
	_, _ = h.bot.Request(callback)

	paymentObj, err := h.paymentService.CheckPaymentStatus(ctx, order.PaymentID)
	if err != nil {
		h.logger.Error("Failed to check payment", "error", err)
		return h.sendError(chatID, "Ошибка проверки платежа")
	}

	switch paymentObj.Status {
	case payment.StatusApproved:
		return h.handleSuccessfulPayment(ctx, chatID, order)
	case payment.StatusPending:
		alertConfig := tgbotapi.NewCallbackWithAlert(query.ID, "Платеж еще обрабатывается. Подождите.")
		_, _ = h.bot.Request(alertConfig)
		return nil
	default:
		return h.sendError(chatID, "Платеж отклонен или отменен")
	}
}

func (h *Handler) handleSuccessfulPayment(ctx context.Context, chatID int64, order *orders.PendingOrder) error {
	flowData := &flows.MigrateClientFlowData{
		AdminUserID:         order.AdminUserID,
		AssistantTelegramID: order.AssistantTelegramID,
		ClientWhatsApp:      order.ClientWhatsApp,
		ServerID:            *order.ServerID,
		ServerName:          *order.ServerName,
		TariffID:            order.TariffID,
		TariffName:          order.TariffName,
		MessageID:           order.MessageID,
	}

	if err := h.createMigratedSubscription(ctx, chatID, flowData, &order.PaymentID); err != nil {
		return err
	}

	// Удаляем pending order
	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err)
	}

	return nil
}

func (h *Handler) handlePaymentRefresh(ctx context.Context, query *tgbotapi.CallbackQuery, order *orders.PendingOrder) error {
	chatID := query.Message.Chat.ID

	callback := tgbotapi.NewCallback(query.ID, "Создаём новую ссылку...")
	_, _ = h.bot.Request(callback)

	paymentEntity := payment.Payment{
		UserID: order.AdminUserID,
		Amount: order.TotalAmount,
		Status: payment.StatusPending,
	}

	paymentObj, err := h.paymentService.CreatePayment(ctx, paymentEntity)
	if err != nil {
		return h.sendError(chatID, "Ошибка создания платежа")
	}

	if paymentObj.PaymentURL == nil {
		return h.sendError(chatID, "Ошибка генерации ссылки")
	}

	if err := h.orderService.UpdatePaymentID(ctx, order.ID, paymentObj.ID); err != nil {
		h.logger.Error("Failed to update payment ID", "error", err)
	}

	paymentMsg := fmt.Sprintf(
		"*Заказ на миграцию создан*\n\n"+
			"Клиент: %s\n"+
			"Сервер: %s\n"+
			"Тариф: %s\n"+
			"Сумма: %.2f ₽\n\n"+
			"Ссылка на оплату: [оплатить](%s)",
		order.ClientWhatsApp, *order.ServerName, order.TariffName, order.TotalAmount, *paymentObj.PaymentURL)

	checkButton := tgbotapi.NewInlineKeyboardButtonData("Проверить оплату", fmt.Sprintf("migpay_check:%d", order.ID))
	refreshButton := tgbotapi.NewInlineKeyboardButtonData("Обновить ссылку", fmt.Sprintf("migpay_refresh:%d", order.ID))
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("Отменить", fmt.Sprintf("migpay_cancel:%d", order.ID))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(checkButton),
		tgbotapi.NewInlineKeyboardRow(refreshButton),
		tgbotapi.NewInlineKeyboardRow(cancelButton),
	)

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, paymentMsg)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, _ = h.bot.Send(editMsg)
	}

	return nil
}

func (h *Handler) handlePaymentCancel(ctx context.Context, query *tgbotapi.CallbackQuery, order *orders.PendingOrder) error {
	chatID := query.Message.Chat.ID

	callback := tgbotapi.NewCallback(query.ID, "Отменено")
	_, _ = h.bot.Request(callback)

	if err := h.orderService.DeletePendingOrder(ctx, order.ID); err != nil {
		h.logger.Error("Failed to delete pending order", "error", err)
	}

	if order.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *order.MessageID, "Заказ отменен")
		_, _ = h.bot.Send(editMsg)
	}

	return nil
}

func (h *Handler) createMigratedSubscription(ctx context.Context, chatID int64, data *flows.MigrateClientFlowData, paymentID *int64) error {
	subReq := &subs.MigrateSubscriptionRequest{
		UserID:              data.AdminUserID,
		TariffID:            data.TariffID,
		ServerID:            data.ServerID,
		ClientWhatsApp:      data.ClientWhatsApp,
		CreatedByTelegramID: data.AssistantTelegramID,
	}

	result, err := h.subscriptionService.MigrateSubscription(ctx, subReq)
	if err != nil {
		h.logger.Error("Failed to migrate subscription", "error", err)
		return h.sendError(chatID, "Ошибка создания подписки")
	}

	return h.sendSubscriptionCreated(chatID, result, data)
}

func (h *Handler) sendSubscriptionCreated(chatID int64, result *subs.CreateSubscriptionResult, data *flows.MigrateClientFlowData) error {
	// Упрощённое сообщение - только User ID
	messageText := fmt.Sprintf(
		"*Клиент мигрирован!*\n\n"+
			"Клиент: `%s`\n"+
			"Тариф: %s\n\n"+
			"User ID:\n`%s`",
		data.ClientWhatsApp,
		data.TariffName,
		result.GeneratedUserID,
	)

	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		_, err := h.bot.Send(editMsg)
		h.stateManager.Clear(chatID)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	_, err := h.bot.Send(msg)

	h.stateManager.Clear(chatID)
	return err
}

func (h *Handler) handleCancel(update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, _ = h.bot.Request(callbackConfig)

	if update.CallbackQuery.Message != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "Отменено")
		_, _ = h.bot.Send(editMsg)
	}

	return nil
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

func isValidPhoneNumber(phone string) bool {
	cleaned := normalizePhone(phone)
	match, _ := regexp.MatchString(`^[\+]?[0-9]{10,15}$`, cleaned)
	return match
}

func normalizePhone(phone string) string {
	cleaned := strings.ReplaceAll(phone, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, "(", "")
	cleaned = strings.ReplaceAll(cleaned, ")", "")
	return cleaned
}

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
