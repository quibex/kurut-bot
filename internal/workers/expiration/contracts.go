package expiration

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/submessages"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
)

type (
	// Storage provides database operations
	Storage interface {
		ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error)
		ListExpiringTodayGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error)
		ListExpiringByAssistantAndDays(ctx context.Context, daysUntilExpiry int) (map[int64][]*subs.Subscription, error)
		ListOverdueSubscriptionsGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error)
		UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
	}

	// NotificationService provides notification functionality
	NotificationService interface {
		SendOverdueSubscriptionMessage(ctx context.Context, chatID int64, sub *subs.Subscription) error
		SendExpiringSubscriptionMessage(ctx context.Context, chatID int64, sub *subs.Subscription, daysUntilExpiry int) error
	}

	// ServerStorage provides server operations
	ServerStorage interface {
		GetServer(ctx context.Context, criteria servers.GetCriteria) (*servers.Server, error)
	}

	// MessageStorage provides subscription message operations
	MessageStorage interface {
		CreateSubscriptionMessage(ctx context.Context, msg submessages.SubscriptionMessage) (*submessages.SubscriptionMessage, error)
	}

	TelegramBot interface {
		Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	}

	TariffService interface {
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	}
)
