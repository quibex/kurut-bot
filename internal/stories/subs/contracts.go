package subs

import (
	"context"
)

type Storage interface {
	ListSubscriptions(ctx context.Context, criteria ListCriteria) ([]*Subscription, error)
	GetSubscription(ctx context.Context, criteria GetCriteria) (*Subscription, error)
	ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
	FindActiveSubscriptionByWhatsApp(ctx context.Context, whatsapp string) (*Subscription, error)
}
