package expiration

import (
	"context"

	"kurut-bot/internal/stories/subs"
)

type (
	// Storage provides database operations
	Storage interface {
		ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error)
		UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error)
	}
)




