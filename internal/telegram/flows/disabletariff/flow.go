package disabletariff

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/tariffs"
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

func (h *Handler) Start(chatID int64) error {
	h.stateManager.SetState(chatID, states.AdminDisableTariffWaitSelection, nil)
	return h.showActiveTariffs(chatID)
}

func (h *Handler) Handle(update *tgbotapi.Update, state states.State) error {
	ctx := context.Background()

	switch state {
	case states.AdminDisableTariffWaitSelection:
		return h.handleTariffSelection(ctx, update)
	default:
		return fmt.Errorf("unknown disable tariff state: %s", state)
	}
}

func (h *Handler) showActiveTariffs(chatID int64) error {
	ctx := context.Background()
	tariffs, err := h.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения тарифов: %w", err)
	}

	if len(tariffs) == 0 {
		h.stateManager.Clear(chatID)
		msg := tgbotapi.NewMessage(chatID, "❌ Активных тарифов нет")
		_, err = h.bot.Send(msg)
		return err
	}

	keyboard := h.createTariffsKeyboard(tariffs)

	msg := tgbotapi.NewMessage(chatID, "📦 Выберите тариф для архивации:")
	msg.ReplyMarkup = keyboard

	_, err = h.bot.Send(msg)
	return err
}

func (h *Handler) createTariffsKeyboard(tariffs []*tariffs.Tariff) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, t := range tariffs {
		text := fmt.Sprintf("📅 %s - %.2f ₽ (%d дней)", t.Name, t.Price, t.DurationDays)
		callbackData := fmt.Sprintf("disable_tariff:%d", t.ID)
		button := tgbotapi.NewInlineKeyboardButtonData(text, callbackData)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel"),
	})

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) handleTariffSelection(ctx context.Context, update *tgbotapi.Update) error {
	if update.CallbackQuery == nil {
		return h.sendError(extractChatID(update), "Пожалуйста, выберите тариф из меню")
	}

	chatID := update.CallbackQuery.Message.Chat.ID

	if update.CallbackQuery.Data == "cancel" {
		return h.handleCancel(ctx, update)
	}

	if !strings.HasPrefix(update.CallbackQuery.Data, "disable_tariff:") {
		return h.sendError(chatID, "Неверные данные тарифа")
	}

	parts := strings.Split(update.CallbackQuery.Data, ":")
	if len(parts) != 2 {
		return h.sendError(chatID, "Неверный формат данных")
	}

	tariffID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return h.sendError(chatID, "Неверный ID тарифа")
	}

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Архивируем тариф...")
	_, err = h.bot.Request(callbackConfig)
	if err != nil {
		return err
	}

	tariff, err := h.tariffService.UpdateTariffStatus(ctx, tariffID, false)
	if err != nil {
		h.logger.Error("Failed to disable tariff", "error", err, "tariffID", tariffID)
		return h.sendError(chatID, "❌ Ошибка архивации тарифа")
	}

	successMsg := fmt.Sprintf("✅ *Тариф архивирован!*\n\n"+
		"📅 **Название:** %s\n"+
		"💰 **Цена:** %.2f ₽\n"+
		"⏰ **Продолжительность:** %d дней\n\n"+
		"📦 Тариф больше не отображается пользователям",
		tariff.Name,
		tariff.Price,
		tariff.DurationDays)

	msg := tgbotapi.NewMessage(chatID, successMsg)

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

	callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Архивация отменена")
	_, err := h.bot.Request(callbackConfig)
	if err != nil {
		h.logger.Error("Failed to answer callback query", "error", err)
	}

	msg := tgbotapi.NewMessage(chatID, "❌ Архивация отменена")
	_, err = h.bot.Send(msg)
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
