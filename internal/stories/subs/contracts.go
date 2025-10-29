package subs

import (
	"context"
	"time"
)

type Storage interface {
	ListSubscriptions(ctx context.Context, criteria ListCriteria) ([]*Subscription, error)
	GetSubscription(ctx context.Context, criteria GetCriteria) (*Subscription, error)
	ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error
}

type MarzbanService interface {
	UpdateUserExpiry(ctx context.Context, marzbanUserID string, newExpiresAt time.Time) error
}
