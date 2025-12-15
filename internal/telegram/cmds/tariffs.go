package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/tariffs"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TariffsCommand struct {
	bot           *tgbotapi.BotAPI
	tariffService tariffService
	statsStorage  TariffsStatsStorage
	logger        *slog.Logger
}

type tariffService interface {
	GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
	GetInactiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error)
	UpdateTariffStatus(ctx context.Context, tariffID int64, isActive bool) (*tariffs.Tariff, error)
}

type TariffsStatsStorage interface {
	GetActiveTariffStatistics(ctx context.Context) ([]storage.TariffStats, error)
	GetArchivedTariffStatistics(ctx context.Context) ([]storage.TariffStats, error)
}

func NewTariffsCommand(
	bot *tgbotapi.BotAPI,
	tariffService tariffService,
	statsStorage TariffsStatsStorage,
	logger *slog.Logger,
) *TariffsCommand {
	return &TariffsCommand{
		bot:           bot,
		tariffService: tariffService,
		statsStorage:  statsStorage,
		logger:        logger,
	}
}

// Execute показывает список тарифов с кнопками управления
func (c *TariffsCommand) Execute(ctx context.Context, chatID int64) error {
	return c.showTariffsList(ctx, chatID, 0)
}

func (c *TariffsCommand) showTariffsList(ctx context.Context, chatID int64, messageID int) error {
	// Получаем статистику по тарифам
	activeStats, err := c.statsStorage.GetActiveTariffStatistics(ctx)
	if err != nil {
		c.logger.Error("Failed to get active tariff stats", "error", err)
		return c.sendError(chatID, "Ошибка получения статистики тарифов")
	}

	archivedStats, err := c.statsStorage.GetArchivedTariffStatistics(ctx)
	if err != nil {
		c.logger.Error("Failed to get archived tariff stats", "error", err)
		return c.sendError(chatID, "Ошибка получения статистики тарифов")
	}

	// Получаем полные данные тарифов
	activeTariffs, err := c.tariffService.GetActiveTariffs(ctx)
	if err != nil {
		c.logger.Error("Failed to get active tariffs", "error", err)
		return c.sendError(chatID, "Ошибка получения тарифов")
	}

	inactiveTariffs, err := c.tariffService.GetInactiveTariffs(ctx)
	if err != nil {
		c.logger.Error("Failed to get inactive tariffs", "error", err)
		return c.sendError(chatID, "Ошибка получения тарифов")
	}

	// Создаем map для быстрого поиска статистики
	statsMap := make(map[int64]int)
	for _, s := range activeStats {
		statsMap[s.TariffID] = s.UserCount
	}
	for _, s := range archivedStats {
		statsMap[s.TariffID] = s.UserCount
	}

	// Считаем общее количество пользователей для процентов
	totalUsers := 0
	for _, count := range statsMap {
		totalUsers += count
	}

	// Формируем текст
	var text strings.Builder
	text.WriteString("📋 *Управление тарифами*\n\n")

	if len(activeTariffs) > 0 {
		text.WriteString("*Активные тарифы:*\n")
		for _, t := range activeTariffs {
			userCount := statsMap[t.ID]
			percent := 0.0
			if totalUsers > 0 {
				percent = float64(userCount) / float64(totalUsers) * 100
			}
			text.WriteString(fmt.Sprintf("• %s (%d дн., %.0f₽): *%d* чел. (%.0f%%)\n",
				t.Name, t.DurationDays, t.Price, userCount, percent))
		}
		text.WriteString("\n")
	} else {
		text.WriteString("_Нет активных тарифов_\n\n")
	}

	if len(inactiveTariffs) > 0 {
		text.WriteString("*Архивные тарифы:*\n")
		for _, t := range inactiveTariffs {
			userCount := statsMap[t.ID]
			text.WriteString(fmt.Sprintf("• %s (%d дн., %.0f₽): *%d* чел.\n",
				t.Name, t.DurationDays, t.Price, userCount))
		}
		text.WriteString("\n")
	}

	// Создаем кнопки
	var rows [][]tgbotapi.InlineKeyboardButton

	// Кнопка создания тарифа
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Создать тариф", "trf_create"),
	))

	// Кнопки архивации для активных тарифов
	if len(activeTariffs) > 0 {
		for _, t := range activeTariffs {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("📦 Архивировать: %s", t.Name),
					fmt.Sprintf("trf_archive:%d", t.ID),
				),
			))
		}
	}

	// Кнопки восстановления для архивных тарифов
	if len(inactiveTariffs) > 0 {
		for _, t := range inactiveTariffs {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("♻️ Восстановить: %s", t.Name),
					fmt.Sprintf("trf_restore:%d", t.ID),
				),
			))
		}
	}

	// Кнопка назад
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", "main_menu"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	// Отправляем или редактируем сообщение
	if messageID > 0 {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text.String())
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard
		_, err = c.bot.Send(editMsg)
	} else {
		msg := tgbotapi.NewMessage(chatID, text.String())
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		_, err = c.bot.Send(msg)
	}

	return err
}

// HandleCallback обрабатывает callback-запросы для тарифов
func (c *TariffsCommand) HandleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) error {
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID
	data := query.Data

	// Отвечаем на callback сразу
	callback := tgbotapi.NewCallback(query.ID, "")
	_, _ = c.bot.Request(callback)

	switch {
	case data == "trf_create":
		// Этот callback будет обработан в router для запуска flow создания тарифа
		return nil

	case strings.HasPrefix(data, "trf_archive:"):
		tariffIDStr := strings.TrimPrefix(data, "trf_archive:")
		tariffID, err := strconv.ParseInt(tariffIDStr, 10, 64)
		if err != nil {
			return c.sendError(chatID, "Неверный ID тарифа")
		}
		return c.archiveTariff(ctx, chatID, messageID, tariffID)

	case strings.HasPrefix(data, "trf_restore:"):
		tariffIDStr := strings.TrimPrefix(data, "trf_restore:")
		tariffID, err := strconv.ParseInt(tariffIDStr, 10, 64)
		if err != nil {
			return c.sendError(chatID, "Неверный ID тарифа")
		}
		return c.restoreTariff(ctx, chatID, messageID, tariffID)

	case data == "trf_list":
		return c.showTariffsList(ctx, chatID, messageID)
	}

	return nil
}

func (c *TariffsCommand) archiveTariff(ctx context.Context, chatID int64, messageID int, tariffID int64) error {
	_, err := c.tariffService.UpdateTariffStatus(ctx, tariffID, false)
	if err != nil {
		c.logger.Error("Failed to archive tariff", "error", err, "tariff_id", tariffID)
		return c.sendError(chatID, "Ошибка архивации тарифа")
	}

	// Обновляем список
	return c.showTariffsList(ctx, chatID, messageID)
}

func (c *TariffsCommand) restoreTariff(ctx context.Context, chatID int64, messageID int, tariffID int64) error {
	_, err := c.tariffService.UpdateTariffStatus(ctx, tariffID, true)
	if err != nil {
		c.logger.Error("Failed to restore tariff", "error", err, "tariff_id", tariffID)
		return c.sendError(chatID, "Ошибка восстановления тарифа")
	}

	// Обновляем список
	return c.showTariffsList(ctx, chatID, messageID)
}

func (c *TariffsCommand) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, "❌ "+message)
	_, err := c.bot.Send(msg)
	return err
}
