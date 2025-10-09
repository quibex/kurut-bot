package subs

import "context"

type Storage interface {
	ListSubscriptions(ctx context.Context, criteria ListCriteria) ([]*Subscription, error)
}
