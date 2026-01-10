package cmds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kurut-bot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type StatsCommand struct {
	bot     *tgbotapi.BotAPI
	storage StatisticsStorage
}

type StatisticsStorage interface {
	GetStatistics(ctx context.Context) (*storage.StatisticsData, error)
}

func NewStatsCommand(bot *tgbotapi.BotAPI, storage StatisticsStorage) *StatsCommand {
	return &StatsCommand{
		bot:     bot,
		storage: storage,
	}
}

func (c *StatsCommand) Execute(ctx context.Context, chatID int64) error {
	stats, err := c.storage.GetStatistics(ctx)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении статистики")
		_, _ = c.bot.Send(msg)
		return fmt.Errorf("get statistics: %w", err)
	}

	text := c.formatStatistics(stats)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "stats_refresh"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Тарифы", "trf_list"),
			tgbotapi.NewInlineKeyboardButtonData("Серверы", "srv_list"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	_, err = c.bot.Send(msg)
	return err
}

func (c *StatsCommand) Refresh(ctx context.Context, chatID int64, messageID int) error {
	stats, err := c.storage.GetStatistics(ctx)
	if err != nil {
		return fmt.Errorf("get statistics: %w", err)
	}

	text := c.formatStatistics(stats)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "stats_refresh"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Тарифы", "trf_list"),
			tgbotapi.NewInlineKeyboardButtonData("Серверы", "srv_list"),
		),
	)

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "Markdown"
	edit.ReplyMarkup = &keyboard
	_, err = c.bot.Send(edit)
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

func (c *StatsCommand) formatStatistics(stats *storage.StatisticsData) string {
	var text strings.Builder

	text.WriteString("📊 *Статистика*\n\n")

	text.WriteString(fmt.Sprintf("*Активных подписок:* %d\n\n", stats.ActiveSubscriptionsCount))

	if len(stats.ActiveTariffStats) > 0 {
		text.WriteString("*Активные тарифы:*\n")
		for _, tariffStat := range stats.ActiveTariffStats {
			text.WriteString(fmt.Sprintf("• %s: *%d* чел.\n", tariffStat.TariffName, tariffStat.UserCount))
		}
		text.WriteString("\n")
	}

	if stats.ArchivedTariffUsersCount > 0 {
		text.WriteString(fmt.Sprintf("*Архивные тарифы:* %d чел.\n\n", stats.ArchivedTariffUsersCount))
	}

	now := time.Now()
	currentMonth := getMonthName(now.Month())
	previousMonth := getMonthName(now.AddDate(0, -1, 0).Month())

	text.WriteString("💰 *Выручка:*\n")
	text.WriteString(fmt.Sprintf("• Сегодня: *%.2f ₽*\n", stats.TodayRevenue))
	text.WriteString(fmt.Sprintf("• Вчера: *%.2f ₽*\n", stats.YesterdayRevenue))
	text.WriteString(fmt.Sprintf("• Средняя за день (%s): *%.2f ₽*\n", currentMonth, stats.AverageRevenuePerDay))
	text.WriteString(fmt.Sprintf("• За %s: *%.2f ₽*\n", previousMonth, stats.PreviousMonthRevenue))
	text.WriteString(fmt.Sprintf("• За %s: *%.2f ₽*\n", currentMonth, stats.CurrentMonthRevenue))

	return text.String()
}

func getMonthName(month time.Month) string {
	months := map[time.Month]string{
		time.January:   "январь",
		time.February:  "февраль",
		time.March:     "март",
		time.April:     "апрель",
		time.May:       "май",
		time.June:      "июнь",
		time.July:      "июль",
		time.August:    "август",
		time.September: "сентябрь",
		time.October:   "октябрь",
		time.November:  "ноябрь",
		time.December:  "декабрь",
	}
	return months[month]
}
