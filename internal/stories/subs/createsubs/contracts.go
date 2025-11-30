package createsubs

import (
	"context"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/wireguard"
)

type (
	storage interface {
		CreateSubscription(ctx context.Context, subscription subs.Subscription) (*subs.Subscription, error)
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
		LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error
	}

	wireguardService interface {
		CreateClient(ctx context.Context, userID string) (*wireguard.ClientConfig, error)
	}
)
