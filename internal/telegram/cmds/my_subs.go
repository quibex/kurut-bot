package cmds

import (
	"context"
	"fmt"
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

func (c *MySubsCommand) Execute(ctx context.Context, user *users.User, chatID int64) error {
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

	var text strings.Builder
	text.WriteString(c.l10n.Get(user.Language, "my_subs.title", nil) + "\n\n")

	for _, sub := range subscriptions {
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

	msg := tgbotapi.NewMessage(chatID, text.String())
	msg.ParseMode = "Markdown"
	_, err = c.bot.Send(msg)
	return err
}
