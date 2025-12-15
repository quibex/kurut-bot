package createtariff

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/flows"
	"kurut-bot/internal/telegram/states"
)

type Handler struct {
	bot           botApi
	stateManager  stateManager
	tariffService tariffService
	logger        *slog.Logger
}

func NewHandler(
	bot botApi,
	sm stateManager,
	ts tariffService,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		bot:           bot,
		stateManager:  sm,
		tariffService: ts,
		logger:        logger,
	}
}

// Start начинает флоу создания тарифа (только для админов)
func (h *Handler) Start(chatID int64) error {
	// Инициализируем данные флоу
	flowData := &flows.CreateTariffFlowData{}
	h.stateManager.SetState(chatID, states.AdminCreateTariffWaitName, flowData)

	// Показываем форму ввода названия
	return h.showNameInput(chatID)
}

// Handle обрабатывает текущее состояние
func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminCreateTariffWaitName:
		return h.handleNameInput(ctx, update)
	case states.AdminCreateTariffWaitPrice:
		return h.handlePriceInput(ctx, update)
	case states.AdminCreateTariffWaitDuration:
		return h.handleDurationInput(ctx, update)
	case states.AdminCreateTariffWaitConfirmation:
		return h.handleConfirmation(ctx, update)
	default:
		return fmt.Errorf("unknown create tariff state: %s", state)
	}
}

func (h *Handler) showNameInput(chatID int64) error {
	messageText := "📝 *Создание нового тарифа*\n\n" +
		"Введите название тарифа (например: \"Базовый\", \"Премиум\"):\n\n" +
		"• Максимум 100 символов\n" +
		"• Не должно быть пустым"

	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) handleNameInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Проверяем на отмену через callback
	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Обрабатываем только текстовые сообщения
	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите название тарифа текстом")
	}

	name := strings.TrimSpace(update.Message.Text)

	// Валидация названия
	if len(name) == 0 {
		return h.sendError(chatID, "❌ Название не может быть пустым")
	}
	if len(name) > 100 {
		return h.sendError(chatID, "❌ Название слишком длинное (максимум 100 символов)")
	}

	// Получаем данные флоу
	data, err := h.stateManager.GetCreateTariffData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные
	data.Name = name

	// Переводим в состояние ввода цены
	h.stateManager.SetState(chatID, states.AdminCreateTariffWaitPrice, data)

	// Показываем форму ввода цены
	return h.showPriceInput(chatID, name)
}

func (h *Handler) showPriceInput(chatID int64, tariffName string) error {
	messageText := fmt.Sprintf("📝 *Создание тарифа: %s*\n\n"+
		"💰 Введите цену тарифа в рублях:\n\n"+
		"• От 0 до 10000 рублей (0 = бесплатный)\n"+
		"• Можно с копейками (например: 199.99)",
		tariffName)

	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) handlePriceInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Проверяем на отмену через callback
	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Обрабатываем только текстовые сообщения
	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите цену числом")
	}

	priceStr := strings.TrimSpace(update.Message.Text)
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return h.sendError(chatID, "❌ Неверный формат цены. Введите число (например: 199 или 199.99)")
	}

	// Валидация цены
	if price < 0 {
		return h.sendError(chatID, "❌ Цена не может быть отрицательной")
	}
	if price > 10000 {
		return h.sendError(chatID, "❌ Цена слишком большая (максимум 10000 рублей)")
	}

	// Получаем данные флоу
	data, err := h.stateManager.GetCreateTariffData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные
	data.Price = price

	// Переводим в состояние ввода продолжительности
	h.stateManager.SetState(chatID, states.AdminCreateTariffWaitDuration, data)

	// Показываем форму ввода продолжительности
	return h.showDurationInput(chatID, data.Name, price)
}

func (h *Handler) showDurationInput(chatID int64, tariffName string, price float64) error {
	messageText := fmt.Sprintf("📝 *Создание тарифа: %s*\n\n"+
		"💰 *Цена:* %.2f ₽\n"+
		"⏰ Введите продолжительность тарифа в днях:\n\n"+
		"• От 1 до 365 дней\n"+
		"• Только целые числа",
		tariffName, price)

	keyboard := h.createCancelKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) handleDurationInput(ctx context.Context, update *tgbotapi.Update) error {
	chatID := extractChatID(update)

	// Проверяем на отмену через callback
	if update.CallbackQuery != nil && update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	// Обрабатываем только текстовые сообщения
	if update.Message == nil || update.Message.Text == "" {
		return h.sendError(chatID, "Пожалуйста, введите количество дней числом")
	}

	durationStr := strings.TrimSpace(update.Message.Text)
	duration, err := strconv.Atoi(durationStr)
	if err != nil {
		return h.sendError(chatID, "❌ Неверный формат. Введите целое число дней")
	}

	// Валидация продолжительности
	if duration < 1 {
		return h.sendError(chatID, "❌ Продолжительность должна быть больше 0 дней")
	}
	if duration > 365 {
		return h.sendError(chatID, "❌ Продолжительность слишком большая (максимум 365 дней)")
	}

	// Получаем данные флоу
	data, err := h.stateManager.GetCreateTariffData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	// Обновляем данные
	data.DurationDays = duration

	// Переводим в состояние подтверждения
	h.stateManager.SetState(chatID, states.AdminCreateTariffWaitConfirmation, data)

	// Показываем подтверждение
	return h.showConfirmation(chatID, data)
}

func (h *Handler) showConfirmation(chatID int64, data *flows.CreateTariffFlowData) error {
	messageText := fmt.Sprintf("📋 *Подтверждение создания тарифа*\n\n"+
		"📅 *Название:* %s\n"+
		"💰 *Цена:* %.2f ₽\n"+
		"⏰ *Продолжительность:* %d дней\n\n"+
		"✅ Все данные корректны?",
		data.Name, data.Price, data.DurationDays)

	keyboard := h.createConfirmationKeyboard()

	msg := tgbotapi.NewMessage(chatID, messageText)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"

	_, err := h.bot.Send(msg)
	return err
}

func (h *Handler) handleConfirmation(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Используйте кнопки для выбора")
	}

	chatID := update.CallbackQuery.Message.Chat.ID
	callbackData := update.CallbackQuery.Data

	// Получаем данные флоу
	data, err := h.stateManager.GetCreateTariffData(chatID)
	if err != nil {
		return h.sendError(chatID, "Ошибка получения данных флоу")
	}

	switch callbackData {
	case "confirm_create_tariff":
		return h.createTariffAndFinish(ctx, update, data)
	case "cancel":
		return h.handleCancel(ctx, update)
	default:
		return h.sendError(chatID, "Неизвестная команда")
	}
}

func (h *Handler) createTariffAndFinish(ctx context.Context, update *tgbotapi.Update, data *flows.CreateTariffFlowData) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	// Создаем тариф
	tariff := tariffs.Tariff{
		Name:           data.Name,
		DurationDays:   data.DurationDays,
		Price:          data.Price,
		TrafficLimitGB: data.TrafficLimitGB,
		IsActive:       true,
	}

	createdTariff, err := h.tariffService.CreateTariff(ctx, tariff)
	if err != nil {
		h.logger.Error("Failed to create tariff", "error", err)
		return h.sendError(chatID, "❌ Ошибка создания тарифа")
	}

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Тариф создан успешно!")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		h.logger.Error("Failed to answer callback query", "error", err)
	}

	// Отправляем сообщение об успешном создании
	successMsg := fmt.Sprintf("✅ *Тариф создан успешно!*\n\n"+
		"📅 *Название:* %s\n"+
		"💰 *Цена:* %.2f ₽\n"+
		"⏰ *Продолжительность:* %d дней\n"+
		"📅 *Создан:* %s",
		createdTariff.Name,
		createdTariff.Price,
		createdTariff.DurationDays,
		createdTariff.CreatedAt.Format("02.01.2006 15:04"))

	msg := tgbotapi.NewMessage(chatID, successMsg)
	msg.ParseMode = "Markdown"

	_, err = h.bot.Send(msg)
	if err != nil {
		h.logger.Error("Failed to send success message", "error", err)
	}

	// Очищаем состояние пользователя
	h.stateManager.Clear(chatID)

	return nil
}

func (h *Handler) handleCancel(ctx context.Context, update *tgbotapi.Update) error {
	chatID := update.CallbackQuery.Message.Chat.ID

	h.stateManager.Clear(chatID)

	// Отвечаем на callback query
	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Создание тарифа отменено")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		h.logger.Error("Failed to answer callback query", "error", err)
	}

	// Отправляем сообщение об отмене
	msg := tgbotapi.NewMessage(chatID, "❌ Создание тарифа отменено")
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
			tgbotapi.NewInlineKeyboardButtonData("✅ Создать тариф", "confirm_create_tariff"),
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
