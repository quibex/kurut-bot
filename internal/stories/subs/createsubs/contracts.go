package createsubs

import (
	"context"

	"kurut-bot/internal/stories/servers"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
)

type storage interface {
	CreateSubscription(ctx context.Context, subscription subs.Subscription) (*subs.Subscription, error)
	GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error
	UpdateSubscriptionGeneratedUserID(ctx context.Context, subscriptionID int64, generatedUserID string) error
	GetAvailableServer(ctx context.Context) (*servers.Server, error)
	GetServerByID(ctx context.Context, serverID int64) (*servers.Server, error)
	IncrementServerUsers(ctx context.Context, serverID int64) error
}
