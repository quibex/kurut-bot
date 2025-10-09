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
