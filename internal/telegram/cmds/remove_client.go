package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"kurut-bot/internal/stories/servers"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type RemoveClientCommand struct {
	bot           *tgbotapi.BotAPI
	serverService removeClientServerService
	logger        *slog.Logger
}

type removeClientServerService interface {
	ListServers(ctx context.Context, criteria servers.ListCriteria) ([]*servers.Server, error)
	DecrementServerUsers(ctx context.Context, serverID int64) error
}

func NewRemoveClientCommand(
	bot *tgbotapi.BotAPI,
	serverService removeClientServerService,
	logger *slog.Logger,
) *RemoveClientCommand {
	return &RemoveClientCommand{
		bot:           bot,
		serverService: serverService,
		logger:        logger,
	}
}

// Execute показывает список серверов для удаления клиента
func (c *RemoveClientCommand) Execute(ctx context.Context, chatID int64) error {
	return c.showServersList(ctx, chatID, 0)
}

func (c *RemoveClientCommand) showServersList(ctx context.Context, chatID int64, messageID int) error {
	// Получаем активные серверы
	notArchived := false
	activeServers, err := c.serverService.ListServers(ctx, servers.ListCriteria{
		Archived: &notArchived,
		Limit:    100,
	})
	if err != nil {
		c.logger.Error("Failed to list servers", "error", err)
		return c.sendError(chatID, "Ошибка получения списка серверов")
	}

	if len(activeServers) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Нет активных серверов")
		_, err = c.bot.Send(msg)
		return err
	}

	// Формируем текст
	var text strings.Builder
	text.WriteString("🗑 *Удаление клиента с сервера*\n\n")
	text.WriteString("Выберите сервер, с которого ушёл клиент.\n")
	text.WriteString("Счётчик пользователей уменьшится на 1.\n\n")

	text.WriteString("*Серверы:*\n")
	for _, s := range activeServers {
		percent := 0.0
		if s.MaxUsers > 0 {
			percent = float64(s.CurrentUsers) / float64(s.MaxUsers) * 100
		}
		text.WriteString(fmt.Sprintf("• *%s:* %d/%d (%.0f%%)\n",
			s.Name, s.CurrentUsers, s.MaxUsers, percent))
	}

	// Создаем кнопки
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, s := range activeServers {
		if s.CurrentUsers > 0 {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("➖ %s (%d)", s.Name, s.CurrentUsers),
					fmt.Sprintf("rmc_dec:%d", s.ID),
				),
			))
		}
	}

	// Кнопка отмены
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀️ Отмена", "main_menu"),
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

// HandleCallback обрабатывает callback-запросы для удаления клиента
func (c *RemoveClientCommand) HandleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) error {
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID
	data := query.Data

	// Отвечаем на callback сразу
	callback := tgbotapi.NewCallback(query.ID, "")
	_, _ = c.bot.Request(callback)

	if strings.HasPrefix(data, "rmc_dec:") {
		serverIDStr := strings.TrimPrefix(data, "rmc_dec:")
		serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
		if err != nil {
			return c.sendError(chatID, "Неверный ID сервера")
		}
		return c.decrementServer(ctx, chatID, messageID, serverID)
	}

	return nil
}

func (c *RemoveClientCommand) decrementServer(ctx context.Context, chatID int64, messageID int, serverID int64) error {
	err := c.serverService.DecrementServerUsers(ctx, serverID)
	if err != nil {
		c.logger.Error("Failed to decrement server users", "error", err, "server_id", serverID)
		return c.sendError(chatID, "Ошибка уменьшения счётчика")
	}

	// Обновляем список
	return c.showServersList(ctx, chatID, messageID)
}

func (c *RemoveClientCommand) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, "❌ "+message)
	_, err := c.bot.Send(msg)
	return err
}
