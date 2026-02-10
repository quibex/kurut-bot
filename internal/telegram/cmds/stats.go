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
	GetCustomerAnalytics(ctx context.Context) (*storage.CustomerAnalytics, error)
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
			tgbotapi.NewInlineKeyboardButtonData("📊 Аналитика", "stats_analytics"),
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
			tgbotapi.NewInlineKeyboardButtonData("📊 Аналитика", "stats_analytics"),
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

	text.WriteString("\n👥 *Новые клиенты:*\n")
	text.WriteString(fmt.Sprintf("• Эта неделя: *%d*\n", stats.NewCustomersThisWeek))
	text.WriteString(fmt.Sprintf("• Прошлая неделя: *%d*\n", stats.NewCustomersLastWeek))

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

func (c *StatsCommand) ShowAnalytics(ctx context.Context, chatID int64, messageID int) error {
	analytics, err := c.storage.GetCustomerAnalytics(ctx)
	if err != nil {
		return fmt.Errorf("get customer analytics: %w", err)
	}

	text := c.formatAnalytics(analytics)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", "stats_analytics_refresh"),
			tgbotapi.NewInlineKeyboardButtonData("📋 Обзор", "stats_overview"),
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

func (c *StatsCommand) RefreshAnalytics(ctx context.Context, chatID int64, messageID int) error {
	return c.ShowAnalytics(ctx, chatID, messageID)
}

func (c *StatsCommand) formatAnalytics(analytics *storage.CustomerAnalytics) string {
	var text strings.Builder

	text.WriteString("📊 *Аналитика клиентов*\n\n")

	// New customers section
	text.WriteString("👥 *Новые клиенты:*\n")

	weekGrowthStr := formatGrowth(analytics.WeekOverWeekGrowth)
	text.WriteString(fmt.Sprintf("• Эта неделя: *%d* %s\n", analytics.NewCustomersThisWeek, weekGrowthStr))
	text.WriteString(fmt.Sprintf("• Прошлая неделя: *%d*\n", analytics.NewCustomersLastWeek))
	text.WriteString(fmt.Sprintf("• Этот месяц: *%d*\n", analytics.NewCustomersThisMonth))
	text.WriteString(fmt.Sprintf("• Прошлый месяц: *%d*\n\n", analytics.NewCustomersLastMonth))

	// Retention section
	text.WriteString("🔄 *Удержание:*\n")
	text.WriteString(fmt.Sprintf("• Продлили: *%d из %d* (%.1f%%)\n", analytics.RenewedCount, analytics.TotalMature, analytics.RenewalRate))
	text.WriteString(fmt.Sprintf("• Отток: *%d из %d* (%.1f%%)\n", analytics.ChurnedCount, analytics.TotalMature, analytics.ChurnRate))
	text.WriteString(fmt.Sprintf("• Надо отключить: *%d из %d* (%.1f%%)\n\n", analytics.PendingDisableCount, analytics.TotalMature, analytics.PendingDisableRate))

	// Metrics section
	text.WriteString("💰 *Метрики:*\n")
	text.WriteString(fmt.Sprintf("• ARPU (выручка/клиент): *%.2f ₽*\n", analytics.ARPU))
	text.WriteString(fmt.Sprintf("• Конверсия trial: *%.1f%%*\n", analytics.TrialConversionRate))

	return text.String()
}

func formatGrowth(growth float64) string {
	if growth > 0 {
		return fmt.Sprintf("↑ %.1f%%", growth)
	} else if growth < 0 {
		return fmt.Sprintf("↓ %.1f%%", -growth)
	}
	return ""
}
