package cmds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type PartnershipCommand struct {
	bot     *tgbotapi.BotAPI
	storage PartnershipStorage
}

type PartnershipStorage interface {
	GetPartnershipStats(ctx context.Context, start, end time.Time, limit int) ([]storage.ReferrerStats, error)
}

func NewPartnershipCommand(bot *tgbotapi.BotAPI, storage PartnershipStorage) *PartnershipCommand {
	return &PartnershipCommand{
		bot:     bot,
		storage: storage,
	}
}

func (c *PartnershipCommand) Execute(ctx context.Context, chatID int64) error {
	return c.showStats(ctx, chatID, 0, true)
}

func (c *PartnershipCommand) HandleCallback(ctx context.Context, callback *tgbotapi.CallbackQuery) error {
	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID
	data := callback.Data

	callbackConfig := tgbotapi.NewCallback(callback.ID, "")
	_, _ = c.bot.Request(callbackConfig)

	switch data {
	case "partner_refresh":
		// Определяем какую неделю показывать по текущему тексту сообщения
		isThisWeek := !strings.Contains(callback.Message.Text, "прошлая неделя")
		return c.showStats(ctx, chatID, messageID, isThisWeek)
	case "partner_this_week":
		return c.showStats(ctx, chatID, messageID, true)
	case "partner_last_week":
		return c.showStats(ctx, chatID, messageID, false)
	}
	return nil
}

func (c *PartnershipCommand) showStats(ctx context.Context, chatID int64, messageID int, thisWeek bool) error {
	now := time.Now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	thisWeekStart := todayStart.AddDate(0, 0, -(weekday - 1))
	lastWeekStart := thisWeekStart.AddDate(0, 0, -7)

	var weekStart, weekEnd time.Time
	var title string
	if thisWeek {
		weekStart = thisWeekStart
		weekEnd = now
		title = "🤝 *Партнёры — эта неделя*"
	} else {
		weekStart = lastWeekStart
		weekEnd = thisWeekStart
		title = "🤝 *Партнёры — прошлая неделя*"
	}

	stats, err := c.storage.GetPartnershipStats(ctx, weekStart, weekEnd, 20)
	if err != nil {
		return fmt.Errorf("get partnership stats: %w", err)
	}

	text := c.formatStats(title, stats)

	var toggleButton tgbotapi.InlineKeyboardButton
	if thisWeek {
		toggleButton = tgbotapi.NewInlineKeyboardButtonData("📅 Прошлая неделя", "partner_last_week")
	} else {
		toggleButton = tgbotapi.NewInlineKeyboardButtonData("📅 Эта неделя", "partner_this_week")
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "partner_refresh"),
			toggleButton,
		),
	)

	if messageID > 0 {
		edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
		edit.ParseMode = "Markdown"
		edit.ReplyMarkup = &keyboard
		_, err = c.bot.Send(edit)
		if err != nil && strings.Contains(err.Error(), "message is not modified") {
			return nil
		}
		return err
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err = c.bot.Send(msg)
	return err
}

func (c *PartnershipCommand) formatStats(title string, stats []storage.ReferrerStats) string {
	var text strings.Builder
	text.WriteString(title + "\n\n")

	if len(stats) == 0 {
		text.WriteString("Пока нет приглашений от партнёров")
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

func getPluralForm(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "приглашение"
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return "приглашения"
	}
	return "приглашений"
}
