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
}

type SubscriptionService interface {
	ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error)
}

type TariffService interface {
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
}

func NewMySubsCommand(bot *tgbotapi.BotAPI, subscriptionSvc SubscriptionService, tariffSvc TariffService) *MySubsCommand {
	return &MySubsCommand{
		bot:             bot,
		subscriptionSvc: subscriptionSvc,
		tariffSvc:       tariffSvc,
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
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка при получении подписок. Попробуйте позже.")
		_, _ = c.bot.Send(msg)
		return fmt.Errorf("list subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📭 У вас пока нет активных подписок.\n\nИспользуйте /buy для покупки подписки.")
		_, _ = c.bot.Send(msg)
		return nil
	}

	var text strings.Builder
	text.WriteString("📋 Ваши активные подписки:\n\n")

	for i, sub := range subscriptions {
		tariff, err := c.tariffSvc.GetTariff(ctx, tariffs.GetCriteria{
			ID: lo.ToPtr(sub.TariffID),
		})
		if err != nil {
			continue
		}

		text.WriteString(fmt.Sprintf("🔹 Подписка #%d\n", i+1))
		text.WriteString(fmt.Sprintf("📦 Тариф: %s\n", tariff.Name))

		if tariff.TrafficLimitGB != nil {
			text.WriteString(fmt.Sprintf("📊 Трафик: %d ГБ\n", *tariff.TrafficLimitGB))
		} else {
			text.WriteString("📊 Трафик: безлимитный\n")
		}

		if sub.ExpiresAt != nil {
			daysLeft := int(time.Until(*sub.ExpiresAt).Hours() / 24)
			if daysLeft > 0 {
				text.WriteString(fmt.Sprintf("⏱ Осталось дней: %d\n", daysLeft))
				text.WriteString(fmt.Sprintf("📅 Действует до: %s\n", sub.ExpiresAt.Format("02.01.2006")))
			} else {
				text.WriteString("⚠️ Подписка истекает сегодня\n")
			}
		}

		if sub.MarzbanLink != "" {
			text.WriteString(fmt.Sprintf("\n🔗 Ваш ключ:\n`%s`\n", sub.MarzbanLink))
		}

		text.WriteString("\n")
	}

	text.WriteString("💡 Для продления подписки используйте /buy")

	msg := tgbotapi.NewMessage(chatID, text.String())
	msg.ParseMode = "Markdown"
	_, err = c.bot.Send(msg)
	return err
}
