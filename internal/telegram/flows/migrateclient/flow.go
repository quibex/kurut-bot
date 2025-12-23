package migrateclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

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
	logger              *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	srvs serverService,
	ss subscriptionService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                 bot,
		stateManager:        sm,
		tariffService:       ts,
		serverService:       srvs,
		subscriptionService: ss,
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

	msg := tgbotapi.NewMessage(chatID, "📱 Введите номер WhatsApp клиента (например: +996555123456):")
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

	// Parse callback: mig_srv:123:ServerName
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
		callbackData := fmt.Sprintf("mig_trf:%d:%s", t.ID, t.Name)
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

	// Parse callback: mig_trf:123:TariffName
	if !strings.HasPrefix(update.CallbackQuery.Data, "mig_trf:") {
		return h.sendError(chatID, "Неверные данные")
	}

	parts := strings.SplitN(update.CallbackQuery.Data, ":", 3)
	if len(parts) != 3 {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	tariffID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID тарифа")
	}
	tariffName := parts[2]

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных")
	}

	flowData.TariffID = tariffID
	flowData.TariffName = tariffName

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создаём подписку...")
	_, _ = h.bot.Request(callbackConfig)

	// Создаём подписку (без увеличения счётчика сервера)
	return h.createMigratedSubscription(ctx, chatID, flowData)
}

func (h *Handler) createMigratedSubscription(ctx context.Context, chatID int64, data *flows.MigrateClientFlowData) error {
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
	passwordLine := ""
	if result.ServerUIPassword != nil && *result.ServerUIPassword != "" {
		passwordLine = fmt.Sprintf("\n`%s`", *result.ServerUIPassword)
	}

	messageText := fmt.Sprintf(
		"*Клиент мигрирован!*\n\n"+
			"Клиент: `%s`\n"+
			"Сервер: %s\n"+
			"Тариф: %s\n\n"+
			"User ID:\n`%s`\n"+
			"Пароль:%s",
		data.ClientWhatsApp,
		data.ServerName,
		data.TariffName,
		result.GeneratedUserID,
		passwordLine,
	)

	whatsappLink := generateWhatsAppLink(data.ClientWhatsApp, "Ваша подписка VPN активирована!")

	var rows [][]tgbotapi.InlineKeyboardButton

	if result.ServerUIURL != nil && *result.ServerUIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Открыть панель", *result.ServerUIURL),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("Написать клиенту", whatsappLink),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if data.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *data.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err := h.bot.Send(editMsg)
		if err != nil {
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
	cleaned := strings.ReplaceAll(phone, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	match, _ := regexp.MatchString(`^[\+]?[0-9]{10,15}$`, cleaned)
	return match
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

func generateWhatsAppLink(phone string, message string) string {
	cleanPhone := strings.TrimPrefix(phone, "+")
	cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "-", "")
	return fmt.Sprintf("https://wa.me/%s?text=%s", cleanPhone, url.QueryEscape(message))
}
