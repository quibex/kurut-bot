package cmds

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/stories/users"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/samber/lo"
)

type MySubsCommand struct {
	bot             *tgbotapi.BotAPI
	subscriptionSvc SubscriptionService
	tariffSvc       TariffService
	l10n            Localizer
}

type SubscriptionService interface {
	ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error)
}

type TariffService interface {
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
}

type Localizer interface {
	Get(lang, key string, params map[string]interface{}) string
}

func NewMySubsCommand(bot *tgbotapi.BotAPI, subscriptionSvc SubscriptionService, tariffSvc TariffService, l10n Localizer) *MySubsCommand {
	return &MySubsCommand{
		bot:             bot,
		subscriptionSvc: subscriptionSvc,
		tariffSvc:       tariffSvc,
		l10n:            l10n,
	}
}

const pageSize = 2

func (c *MySubsCommand) Execute(ctx context.Context, user *users.User, chatID int64) error {
	return c.showPage(ctx, user, chatID, 0, 0, false)
}

func (c *MySubsCommand) HandleCallback(ctx context.Context, user *users.User, chatID int64, messageID int, callbackData string) error {
	if !strings.HasPrefix(callbackData, "my_subs_page:") {
		return fmt.Errorf("invalid callback data")
	}

	parts := strings.Split(callbackData, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid callback format")
	}

	page, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid page number: %w", err)
	}

	return c.showPage(ctx, user, chatID, page, messageID, true)
}

func (c *MySubsCommand) showPage(ctx context.Context, user *users.User, chatID int64, page, messageID int, isEdit bool) error {
	activeStatus := []subs.Status{subs.StatusActive}
	subscriptions, err := c.subscriptionSvc.ListSubscriptions(ctx, subs.ListCriteria{
		UserIDs: []int64{user.ID},
		Status:  activeStatus,
		Limit:   50,
	})
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, c.l10n.Get(user.Language, "my_subs.error_loading", nil))
		_, _ = c.bot.Send(msg)
		return fmt.Errorf("list subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, c.l10n.Get(user.Language, "my_subs.no_subscriptions", nil))
		_, _ = c.bot.Send(msg)
		return nil
	}

	// Calculate pagination
	totalPages := (len(subscriptions) + pageSize - 1) / pageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	startIdx := page * pageSize
	endIdx := startIdx + pageSize
	if endIdx > len(subscriptions) {
		endIdx = len(subscriptions)
	}

	var text strings.Builder
	text.WriteString(c.l10n.Get(user.Language, "my_subs.title", nil) + "\n\n")

	// Show only subscriptions for current page
	for i := startIdx; i < endIdx; i++ {
		sub := subscriptions[i]
		tariff, err := c.tariffSvc.GetTariff(ctx, tariffs.GetCriteria{
			ID: lo.ToPtr(sub.TariffID),
		})
		if err != nil {
			continue
		}

		text.WriteString(c.l10n.Get(user.Language, "my_subs.subscription_id", map[string]interface{}{
			"id": sub.ID,
		}) + "\n")

		text.WriteString(c.l10n.Get(user.Language, "my_subs.tariff", map[string]interface{}{
			"name": tariff.Name,
		}) + "\n")

		if sub.ClientName != nil && *sub.ClientName != "" {
			text.WriteString(c.l10n.Get(user.Language, "my_subs.client", map[string]interface{}{
				"name": *sub.ClientName,
			}) + "\n")
		}

		if tariff.TrafficLimitGB != nil {
			text.WriteString(c.l10n.Get(user.Language, "my_subs.traffic_limit", map[string]interface{}{
				"gb": *tariff.TrafficLimitGB,
			}) + "\n")
		} else {
			text.WriteString(c.l10n.Get(user.Language, "my_subs.traffic_unlimited", nil) + "\n")
		}

		if sub.ExpiresAt != nil {
			daysLeft := int(time.Until(*sub.ExpiresAt).Hours() / 24)
			if daysLeft > 0 {
				text.WriteString(c.l10n.Get(user.Language, "my_subs.days_left", map[string]interface{}{
					"days": daysLeft,
				}) + "\n")
				text.WriteString(c.l10n.Get(user.Language, "my_subs.expires_at", map[string]interface{}{
					"date": sub.ExpiresAt.Format("02.01.2006"),
				}) + "\n")
			} else {
				text.WriteString(c.l10n.Get(user.Language, "my_subs.expires_today", nil) + "\n")
			}
		}

		if sub.MarzbanLink != "" {
			text.WriteString("\n" + c.l10n.Get(user.Language, "my_subs.your_key", nil) + "\n`" + sub.MarzbanLink + "`\n")
		}

		text.WriteString("\n")
	}

	text.WriteString("\n💡 " + c.l10n.Get(user.Language, "my_subs.renew_note", nil))

	// Add navigation buttons if needed
	keyboard := c.createNavigationKeyboard(user.Language, page, totalPages)

	if isEdit {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text.String())
		editMsg.ParseMode = "Markdown"
		if keyboard != nil {
			editMsg.ReplyMarkup = keyboard
		}
		_, err = c.bot.Send(editMsg)
		return err
	}

	msg := tgbotapi.NewMessage(chatID, text.String())
	msg.ParseMode = "Markdown"
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	_, err = c.bot.Send(msg)
	return err
}

func (c *MySubsCommand) createNavigationKeyboard(lang string, page, totalPages int) *tgbotapi.InlineKeyboardMarkup {
	if totalPages <= 1 {
		return nil
	}

	var navButtons []tgbotapi.InlineKeyboardButton
	if page > 0 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("⬅️", fmt.Sprintf("my_subs_page:%d", page-1)))
	}
	navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "my_subs_noop"))
	if page < totalPages-1 {
		navButtons = append(navButtons, tgbotapi.NewInlineKeyboardButtonData("➡️", fmt.Sprintf("my_subs_page:%d", page+1)))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(navButtons)
	return &keyboard
}
