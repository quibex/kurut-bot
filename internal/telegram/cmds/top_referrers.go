package cmds

import (
	"context"
	"fmt"
	"strings"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TopReferrersCommand struct {
	bot     *tgbotapi.BotAPI
	storage TopReferrersStorage
}

type TopReferrersStorage interface {
	GetTopReferrersThisWeek(ctx context.Context, limit int) ([]storage.ReferrerStats, error)
}

func NewTopReferrersCommand(bot *tgbotapi.BotAPI, storage TopReferrersStorage) *TopReferrersCommand {
	return &TopReferrersCommand{
		bot:     bot,
		storage: storage,
	}
}

func (c *TopReferrersCommand) Execute(ctx context.Context, chatID int64) error {
	stats, err := c.storage.GetTopReferrersThisWeek(ctx, 10)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении топа рефералов")
		_, _ = c.bot.Send(msg)
		return fmt.Errorf("get top referrers: %w", err)
	}

	text := c.formatTopReferrers(stats)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "top_ref_refresh"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err = c.bot.Send(msg)
	return err
}

func (c *TopReferrersCommand) Refresh(ctx context.Context, chatID int64, messageID int) error {
	stats, err := c.storage.GetTopReferrersThisWeek(ctx, 10)
	if err != nil {
		return fmt.Errorf("get top referrers: %w", err)
	}

	text := c.formatTopReferrers(stats)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "top_ref_refresh"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "Markdown"
	edit.ReplyMarkup = &keyboard
	_, err = c.bot.Send(edit)
	return err
}

func (c *TopReferrersCommand) formatTopReferrers(stats []storage.ReferrerStats) string {
	var text strings.Builder

	text.WriteString("🏆 *Топ-10 за неделю*\n\n")

	if len(stats) == 0 {
		text.WriteString("За эту неделю ещё нет приглашений")
		return text.String()
	}

	for i, stat := range stats {
		medal := ""
		switch i {
		case 0:
			medal = "🥇"
		case 1:
			medal = "🥈"
		case 2:
			medal = "🥉"
		default:
			medal = fmt.Sprintf("%d.", i+1)
		}

		suffix := getPluralForm(stat.Count)
		text.WriteString(fmt.Sprintf("%s `%s` — *%d* %s\n", medal, stat.ReferrerWhatsApp, stat.Count, suffix))
	}

	return text.String()
}

func getPluralForm(count int) string {
	if count%10 == 1 && count%100 != 11 {
		return "приглашение"
	}
	if count%10 >= 2 && count%10 <= 4 && (count%100 < 10 || count%100 >= 20) {
		return "приглашения"
	}
	return "приглашений"
}

