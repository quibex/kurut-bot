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
		CreatePeer(ctx context.Context, userID int64, peerID string) (*wireguard.PeerConfig, error)
	}
)
