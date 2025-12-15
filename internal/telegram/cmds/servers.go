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

type ServersCommand struct {
	bot           *tgbotapi.BotAPI
	serverService serverService
	logger        *slog.Logger
}

type serverService interface {
	ListServers(ctx context.Context, criteria servers.ListCriteria) ([]*servers.Server, error)
	ArchiveServer(ctx context.Context, serverID int64) (*servers.Server, error)
	UnarchiveServer(ctx context.Context, serverID int64) (*servers.Server, error)
}

func NewServersCommand(
	bot *tgbotapi.BotAPI,
	serverService serverService,
	logger *slog.Logger,
) *ServersCommand {
	return &ServersCommand{
		bot:           bot,
		serverService: serverService,
		logger:        logger,
	}
}

// Execute показывает список серверов с кнопками управления
func (c *ServersCommand) Execute(ctx context.Context, chatID int64) error {
	return c.showServersList(ctx, chatID, 0)
}

func (c *ServersCommand) showServersList(ctx context.Context, chatID int64, messageID int) error {
	// Получаем все серверы
	allServers, err := c.serverService.ListServers(ctx, servers.ListCriteria{Limit: 100})
	if err != nil {
		c.logger.Error("Failed to list servers", "error", err)
		return c.sendError(chatID, "Ошибка получения списка серверов")
	}

	// Разделяем на активные и архивные
	var activeServers, archivedServers []*servers.Server
	for _, s := range allServers {
		if s.Archived {
			archivedServers = append(archivedServers, s)
		} else {
			activeServers = append(activeServers, s)
		}
	}

	// Формируем текст
	var text strings.Builder
	text.WriteString("📡 *Управление серверами*\n\n")

	if len(activeServers) > 0 {
		text.WriteString("*Активные серверы:*\n")
		for _, s := range activeServers {
			percent := 0.0
			if s.MaxUsers > 0 {
				percent = float64(s.CurrentUsers) / float64(s.MaxUsers) * 100
			}
			// Выбираем иконку в зависимости от загрузки
			icon := "🟢"
			if percent >= 80 {
				icon = "🟡"
			}
			if percent >= 95 {
				icon = "🔴"
			}
			text.WriteString(fmt.Sprintf("%s *%s:* %d/%d (%.0f%%)\n",
				icon, s.Name, s.CurrentUsers, s.MaxUsers, percent))
		}
		text.WriteString("\n")
	} else {
		text.WriteString("_Нет активных серверов_\n\n")
	}

	if len(archivedServers) > 0 {
		text.WriteString("*Архивные серверы:*\n")
		for _, s := range archivedServers {
			text.WriteString(fmt.Sprintf("📦 *%s:* %d/%d\n",
				s.Name, s.CurrentUsers, s.MaxUsers))
		}
		text.WriteString("\n")
	}

	// Создаем кнопки
	var rows [][]tgbotapi.InlineKeyboardButton

	// Кнопка добавления сервера
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить сервер", "srv_add"),
	))

	// Кнопки архивации для активных серверов
	if len(activeServers) > 0 {
		for _, s := range activeServers {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("📦 Архивировать: %s", s.Name),
					fmt.Sprintf("srv_archive:%d", s.ID),
				),
			))
		}
	}

	// Кнопки восстановления для архивных серверов
	if len(archivedServers) > 0 {
		for _, s := range archivedServers {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("♻️ Восстановить: %s", s.Name),
					fmt.Sprintf("srv_restore:%d", s.ID),
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

// HandleCallback обрабатывает callback-запросы для серверов
func (c *ServersCommand) HandleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) error {
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID
	data := query.Data

	// Отвечаем на callback сразу
	callback := tgbotapi.NewCallback(query.ID, "")
	_, _ = c.bot.Request(callback)

	switch {
	case data == "srv_add":
		// Этот callback будет обработан в router для запуска flow добавления сервера
		return nil

	case strings.HasPrefix(data, "srv_archive:"):
		serverIDStr := strings.TrimPrefix(data, "srv_archive:")
		serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
		if err != nil {
			return c.sendError(chatID, "Неверный ID сервера")
		}
		return c.archiveServer(ctx, chatID, messageID, serverID)

	case strings.HasPrefix(data, "srv_restore:"):
		serverIDStr := strings.TrimPrefix(data, "srv_restore:")
		serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
		if err != nil {
			return c.sendError(chatID, "Неверный ID сервера")
		}
		return c.restoreServer(ctx, chatID, messageID, serverID)

	case data == "srv_list":
		return c.showServersList(ctx, chatID, messageID)
	}

	return nil
}

func (c *ServersCommand) archiveServer(ctx context.Context, chatID int64, messageID int, serverID int64) error {
	_, err := c.serverService.ArchiveServer(ctx, serverID)
	if err != nil {
		c.logger.Error("Failed to archive server", "error", err, "server_id", serverID)
		return c.sendError(chatID, "Ошибка архивации сервера")
	}

	// Обновляем список
	return c.showServersList(ctx, chatID, messageID)
}

func (c *ServersCommand) restoreServer(ctx context.Context, chatID int64, messageID int, serverID int64) error {
	_, err := c.serverService.UnarchiveServer(ctx, serverID)
	if err != nil {
		c.logger.Error("Failed to restore server", "error", err, "server_id", serverID)
		return c.sendError(chatID, "Ошибка восстановления сервера")
	}

	// Обновляем список
	return c.showServersList(ctx, chatID, messageID)
}

func (c *ServersCommand) sendError(chatID int64, message string) error {
	msg := tgbotapi.NewMessage(chatID, "❌ "+message)
	_, err := c.bot.Send(msg)
	return err
}
