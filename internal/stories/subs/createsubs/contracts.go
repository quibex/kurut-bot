package createsubs

import (
	"context"

	"kurut-bot/internal/marzban"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
)

type (
	storage interface {
		CreateSubscription(ctx context.Context, subscription subs.Subscription) (*subs.Subscription, error)
		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
		LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error
	}

	marzbanService interface {
		CreateUser(ctx context.Context, req marzban.CreateUserRequest) (*marzban.UserSubscription, error)
	}
)
