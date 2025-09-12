package createsubs

import (
	"context"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/pkg/marzban"
)

type (
	storage interface {
		BulkInsertSubscriptions(ctx context.Context, subscriptions []subs.Subscription) ([]subs.Subscription, error)

		GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error)
	}

	marzbanClient interface {
		AddUser(ctx context.Context, request *marzban.UserCreate) (marzban.AddUserRes, error)
		GetInbounds(ctx context.Context) (marzban.GetInboundsRes, error)
	}
)
