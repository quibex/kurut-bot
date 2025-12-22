package cmds

import (
	"context"
	"fmt"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type MySubsCommand struct {
	bot     *tgbotapi.BotAPI
	storage MySubsStorage
}

type MySubsStorage interface {
	GetAssistantStats(ctx context.Context, assistantTelegramID int64) (*storage.AssistantStats, error)
}

func NewMySubsCommand(bot *tgbotapi.BotAPI, storage MySubsStorage) *MySubsCommand {
	return &MySubsCommand{
		bot:     bot,
		storage: storage,
	}
}

func (c *MySubsCommand) Execute(ctx context.Context, assistantTelegramID int64, chatID int64) error {
	stats, err := c.storage.GetAssistantStats(ctx, assistantTelegramID)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки статистики")
		_, _ = c.bot.Send(msg)
		return fmt.Errorf("get assistant stats: %w", err)
	}

	text := fmt.Sprintf(
		"📊 *Ваша статистика*\n\n"+
			"📅 Подключено сегодня: *%d*\n"+
			"📅 Подключено вчера: *%d*\n"+
			"📅 Эта неделя: *%d*\n"+
			"📅 Прошлая неделя: *%d*",
		stats.CreatedToday,
		stats.CreatedYesterday,
		stats.CreatedThisWeek,
		stats.CreatedLastWeek,
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, err = c.bot.Send(msg)
	return err
}
