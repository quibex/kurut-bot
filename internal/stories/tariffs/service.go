package tariffs

import (
	"context"

	"github.com/samber/lo"
)

// Service provides business logic for tariff operations
type Service struct {
	storage Storage
}

// NewService creates a new tariff service
func NewService(storage Storage) *Service {
	return &Service{
		storage: storage,
	}
}

func (s *Service) CreateTariff(ctx context.Context, tariff Tariff) (*Tariff, error) {
	return s.storage.CreateTariff(ctx, tariff)
}

func (s *Service) GetActiveTariffs(ctx context.Context) ([]*Tariff, error) {
	criteria := ListCriteria{
		IsActive: lo.ToPtr(true),
		Limit:    100,
	}
	return s.storage.ListTariffs(ctx, criteria)
}
