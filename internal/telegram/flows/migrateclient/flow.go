package migrateclient

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot                botApi
	stateManager       stateManager
	serverService      serverService
	serverStorage      serverStorage
	clientTokenStorage clientTokenStorage
	webDomain          string
	logger             *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ss serverService,
	srvStorage serverStorage,
	cts clientTokenStorage,
	webDomain string,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:                bot,
		stateManager:       sm,
		serverService:      ss,
		serverStorage:      srvStorage,
		clientTokenStorage: cts,
		webDomain:          webDomain,
		logger:             logger,
	}
}

// Start начинает flow миграции клиента
func (h *Handler) Start(userID, assistantTelegramID, chatID int64) error {
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

	whatsapp = normalizePhone(whatsapp)

	if !isValidPhoneNumber(whatsapp) {
		return h.sendError(chatID, "❌ Неверный формат номера. Введите номер в формате +996555123456")
	}

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	flowData.ClientWhatsApp = whatsapp
	h.stateManager.SetState(chatID, states.AdminMigrateClientWaitServer, flowData)

	return h.showServers(ctx, chatID)
}

// showServers показывает список серверов для выбора
func (h *Handler) showServers(ctx context.Context, chatID int64) error {
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

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range serversList {
		text := fmt.Sprintf("🖥 %s", s.Name)
		callbackData := fmt.Sprintf("mig_srv:%d:%s", s.ID, s.Name)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "mig_cancel"),
	})

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	flowData, _ := h.stateManager.GetMigrateClientData(chatID)
	text := fmt.Sprintf("🖥 Выберите сервер клиента:\n\n📱 Клиент: `%s`", flowData.ClientWhatsApp)

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

// handleServerSelection обработка выбора сервера — генерируем ссылку для клиента
func (h *Handler) handleServerSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		chatID := extractChatID(update)
		return h.sendError(chatID, "Пожалуйста, выберите сервер из списка")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	if callbackData == "mig_cancel" {
		return h.handleCancel(update)
	}

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

	flowData, err := h.stateManager.GetMigrateClientData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	flowData.ServerID = serverID
	flowData.ServerName = serverName

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
	_, _ = h.bot.Request(callbackConfig)

	// Получаем данные сервера (для кнопки панели управления)
	server, err := h.serverStorage.GetServer(ctx, servers.GetCriteria{ID: &serverID})
	if err != nil {
		h.logger.Error("Failed to get server", "error", err, "server_id", serverID)
	}

	// Генерируем личную ссылку для клиента
	clientToken, err := h.clientTokenStorage.GetOrCreateClientToken(ctx, flowData.ClientWhatsApp, flowData.AssistantTelegramID)
	if err != nil {
		h.logger.Error("Failed to create client token", "error", err, "whatsapp", flowData.ClientWhatsApp)
		h.stateManager.Clear(chatID)
		return h.sendError(chatID, "❌ Ошибка создания ссылки для клиента")
	}

	clientLink := fmt.Sprintf("%s/c/%s", h.webDomain, clientToken.Token)

	messageText := fmt.Sprintf(
		"✅ *Ссылка для клиента создана*\n\n"+
			"📱 Клиент: `%s`\n"+
			"🖥 Сервер: %s\n\n"+
			"🔗 Ссылка для клиента:\n`%s`\n\n"+
			"Клиент выберет тариф и оплатит на сайте.",
		flowData.ClientWhatsApp,
		serverName,
		clientLink,
	)

	var rows [][]tgbotapi.InlineKeyboardButton

	if server != nil && server.UIURL != "" {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("🖥 Панель управления", server.UIURL),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if flowData.MessageID != nil {
		editMsg := tgbotapi.NewEditMessageText(chatID, *flowData.MessageID, messageText)
		editMsg.ParseMode = "Markdown"
		editMsg.DisableWebPagePreview = true
		editMsg.ReplyMarkup = &keyboard
		_, err = h.bot.Send(editMsg)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, messageText)
			msg.ParseMode = "Markdown"
			msg.DisableWebPagePreview = true
			msg.ReplyMarkup = keyboard
			_, err = h.bot.Send(msg)
		}
	} else {
		msg := tgbotapi.NewMessage(chatID, messageText)
		msg.ParseMode = "Markdown"
		msg.DisableWebPagePreview = true
		msg.ReplyMarkup = keyboard
		_, err = h.bot.Send(msg)
	}

	h.stateManager.Clear(chatID)
	return err
}

// handleCancel обрабатывает отмену
func (h *Handler) handleCancel(update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Отменено")
	_, _ = h.bot.Request(callbackConfig)

	text := "📱 Доступные команды:\n" +
		"/new_client — Новый клиент\n" +
		"/migrate_client — Мигрировать существующего клиента\n" +
		"/my_subs — Список подписок"

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
	if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
		return update.CallbackQuery.Message.Chat.ID
	}
	return 0
}

func normalizePhone(phone string) string {
	var result strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func isValidPhoneNumber(normalizedPhone string) bool {
	match, _ := regexp.MatchString(`^[0-9]{10,15}$`, normalizedPhone)
	return match
}
