package subs

import "context"

type Service struct {
	storage Storage
}

func NewService(storage Storage) *Service {
	return &Service{
		storage: storage,
	}
}

func (s *Service) ListSubscriptions(ctx context.Context, criteria ListCriteria) ([]*Subscription, error) {
	return s.storage.ListSubscriptions(ctx, criteria)
}

func (s *Service) GetSubscription(ctx context.Context, criteria GetCriteria) (*Subscription, error) {
	return s.storage.GetSubscription(ctx, criteria)
}

func (s *Service) ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error {
	return s.storage.ExtendSubscription(ctx, subscriptionID, additionalDays)
}
