package addserver

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"
)

type Handler struct {
	bot           botApi
	stateManager  stateManager
	serverService serverService
	logger        *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ss serverService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:           bot,
		stateManager:  sm,
		serverService: ss,
		logger:        logger,
	}
}

// Start начинает флоу добавления сервера
func (h *Handler) Start(chatID int64) error {
	flowData := &flows.AddServerFlowData{
		MaxUsers: 150, // Значение по умолчанию
	}
	h.stateManager.SetState(chatID, states.AdminServerWaitName, flowData)

	messageText := "🖥 *Добавление нового сервера*\n\n" +
		"Введите название сервера (например: \"Server 1\", \"RU-1\"):"

	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	_, err := h.bot.Send(msg)
	return err
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminServerWaitName:
		return h.handleNameInput(ctx, update)
	case states.AdminServerWaitURL:
		return h.handleURLInput(ctx, update)
	case states.AdminServerWaitPassword:
		return h.handlePasswordInput(ctx, update)
	case states.AdminServerWaitCurrentUsers:
		return h.handleCurrentUsersInput(ctx, update)
	case states.AdminServerWaitMaxUsers:
		return h.handleMaxUsersInput(ctx, update)
	case states.AdminServerWaitConfirmation:
		return h.handleConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown add server state: %s", state)
	}
}

func (h *Handler) handleNameInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите название сервера текстом")
	}

	name := strings.TrimSpace(update.Message.Text)

	if len(name) == 0 {
		return h.sendError(chatID, "❌ Название не может быть пустым")
	}

	data, err := h.stateManager.GetAddServerData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	data.Name = name
	h.stateManager.SetState(chatID, states.AdminServerWaitURL, data)

	messageText := "🌐 Введите URL панели управления (например: https://wg.example.com):"
	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) handleURLInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите URL текстом")
	}

	urlStr := strings.TrimSpace(update.Message.Text)

	if len(urlStr) == 0 {
		return h.sendError(chatID, "❌ URL не может быть пустым")
	}

	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return h.sendError(chatID, "❌ URL должен начинаться с http:// или https://")
	}

	data, err := h.stateManager.GetAddServerData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	data.UIURL = urlStr
	h.stateManager.SetState(chatID, states.AdminServerWaitPassword, data)

	messageText := "🔑 Введите пароль от панели управления:"
	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) handlePasswordInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите пароль текстом")
	}

	password := strings.TrimSpace(update.Message.Text)

	if len(password) == 0 {
		return h.sendError(chatID, "❌ Пароль не может быть пустым")
	}

	data, err := h.stateManager.GetAddServerData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	data.UIPassword = password
	h.stateManager.SetState(chatID, states.AdminServerWaitCurrentUsers, data)

	messageText := "👥 Введите текущее количество пользователей на сервере (0 если новый сервер):"
	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) handleCurrentUsersInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите число")
	}

	currentUsersStr := strings.TrimSpace(update.Message.Text)
	currentUsers, err := strconv.Atoi(currentUsersStr)
	if err != nil {
		return h.sendError(chatID, "❌ Неверный формат. Введите целое число")
	}

	if currentUsers < 0 {
		return h.sendError(chatID, "❌ Количество пользователей не может быть отрицательным")
	}

	data, err := h.stateManager.GetAddServerData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	data.CurrentUsers = currentUsers
	h.stateManager.SetState(chatID, states.AdminServerWaitMaxUsers, data)

	messageText := "🔢 Введите максимальное количество пользователей (по умолчанию 150):"
	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) handleMaxUsersInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите число")
	}

	maxUsersStr := strings.TrimSpace(update.Message.Text)
	maxUsers, err := strconv.Atoi(maxUsersStr)
	if err != nil {
		return h.sendError(chatID, "❌ Неверный формат. Введите целое число")
	}

	if maxUsers < 1 {
		return h.sendError(chatID, "❌ Максимальное количество должно быть больше 0")
	}

	data, err := h.stateManager.GetAddServerData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	if maxUsers < data.CurrentUsers {
		return h.sendError(chatID, fmt.Sprintf("❌ Максимальное количество (%d) не может быть меньше текущего (%d)", maxUsers, data.CurrentUsers))
	}

	data.MaxUsers = maxUsers
	h.stateManager.SetState(chatID, states.AdminServerWaitConfirmation, data)

	return h.showConfirmation(chatID, data)
}

func (h *Handler) showConfirmation(chatID int64, data *flows.AddServerFlowData) error {
	messageText := fmt.Sprintf("📋 *Подтверждение добавления сервера*\n\n"+
		"🖥 Название: %s\n"+
		"🌐 URL: %s\n"+
		"🔑 Пароль: `%s`\n"+
		"👥 Текущих пользователей: %d\n"+
		"🔢 Максимум пользователей: %d\n\n"+
		"✅ Все данные корректны?",
		data.Name, data.UIURL, data.UIPassword, data.CurrentUsers, data.MaxUsers)

	keyboard := h.createConfirmationKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) handleConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	data, err := h.stateManager.GetAddServerData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	switch callbackData {
	case "confirm_add_server":
		return h.createServerAndFinish(ctx, update, data)
	case "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
}

func (h *Handler) createServerAndFinish(ctx context.Context, update *tgbotapi.Update, data *flows.AddServerFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	server := servers.Server{
		Name:         data.Name,
		UIURL:        data.UIURL,
		UIPassword:   data.UIPassword,
		CurrentUsers: data.CurrentUsers,
		MaxUsers:     data.MaxUsers,
		Archived:     false,
	}

	createdServer, err := h.serverService.CreateServer(ctx, server)
	if err != nil {
		h.logger.Error("Failed to create server", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания сервера")
	}

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Сервер добавлен успешно!")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		h.logger.Error("Failed to answer callback query", "error", err)
	}

	successMsg := fmt.Sprintf("✅ *Сервер добавлен успешно!*\n\n"+
		"🖥 **Название:** %s\n"+
		"🌐 **URL:** %s\n"+
		"👥 **Текущих пользователей:** %d/%d\n"+
		"🆔 **ID:** %d",
		createdServer.Name,
		createdServer.UIURL,
		createdServer.CurrentUsers,
		createdServer.MaxUsers,
		createdServer.ID)

	msg := tgbotapi.NewMessage(chatID, successMsg)
	msg.ParseMode = "Markdown"

	_, err = h.bot.Send(msg)
	if err != nil {
		h.logger.Error("Failed to send success message", "error", err)
	}

	h.stateManager.Clear(chatID)

	return nil
}

func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Добавление сервера отменено")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		h.logger.Error("Failed to answer callback query", "error", err)
	}

	msg := tgbotapi.NewMessage(chatID, "❌ Добавление сервера отменено")
	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) createCancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
		),
	)
}

func (h *Handler) createConfirmationKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Добавить сервер", "confirm_add_server"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
		),
	)
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
