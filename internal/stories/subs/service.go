package subs

import (
	"context"
	"fmt"
)

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
	subscription, err := s.storage.GetSubscription(ctx, GetCriteria{IDs: []int64{subscriptionID}})
	if err != nil {
		return fmt.Errorf("get subscription: %w", err)
	}
	if subscription == nil {
		return fmt.Errorf("subscription not found: %d", subscriptionID)
	}

	if err := s.storage.ExtendSubscription(ctx, subscriptionID, additionalDays); err != nil {
		return fmt.Errorf("extend subscription in DB: %w", err)
	}

	return nil
}

func (s *Service) FindActiveSubscriptionByWhatsApp(ctx context.Context, whatsapp string) (*Subscription, error) {
	return s.storage.FindActiveSubscriptionByWhatsApp(ctx, whatsapp)
}
