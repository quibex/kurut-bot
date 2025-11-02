package subs

import (
	"context"
)

type Storage interface {
	ListSubscriptions(ctx context.Context, criteria ListCriteria) ([]*Subscription, error)
	GetSubscription(ctx context.Context, criteria GetCriteria) (*Subscription, error)
	ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
}

type WireguardService interface {
	DisablePeer(ctx context.Context, subscription *Subscription) error
	EnablePeer(ctx context.Context, subscription *Subscription) error
}
