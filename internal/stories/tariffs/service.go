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
	// Если создаем пробный тариф (price = 0), деактивируем все старые пробные
	if tariff.Price == 0 {
		// Получаем все активные бесплатные тарифы
		allTariffs, err := s.storage.ListTariffs(ctx, ListCriteria{
			IsActive: lo.ToPtr(true),
			Limit:    100,
		})
		if err != nil {
			return nil, err
		}

		// Деактивируем все бесплатные тарифы
		for _, t := range allTariffs {
			if t.Price == 0 {
				_, err := s.storage.UpdateTariff(ctx, GetCriteria{
					ID: lo.ToPtr(t.ID),
				}, UpdateParams{
					IsActive: lo.ToPtr(false),
				})
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return s.storage.CreateTariff(ctx, tariff)
}

func (s *Service) GetActiveTariffs(ctx context.Context) ([]*Tariff, error) {
	criteria := ListCriteria{
		IsActive: lo.ToPtr(true),
		Limit:    100,
	}
	allTariffs, err := s.storage.ListTariffs(ctx, criteria)
	if err != nil {
		return nil, err
	}

	// Фильтруем бесплатные тарифы (они только для пробного периода)
	var paidTariffs []*Tariff
	for _, t := range allTariffs {
		if t.Price > 0 {
			paidTariffs = append(paidTariffs, t)
		}
	}

	return paidTariffs, nil
}

func (s *Service) GetInactiveTariffs(ctx context.Context) ([]*Tariff, error) {
	criteria := ListCriteria{
		IsActive: lo.ToPtr(false),
		Limit:    100,
	}
	return s.storage.ListTariffs(ctx, criteria)
}

func (s *Service) UpdateTariffStatus(ctx context.Context, tariffID int64, isActive bool) (*Tariff, error) {
	criteria := GetCriteria{
		ID: lo.ToPtr(tariffID),
	}
	params := UpdateParams{
		IsActive: lo.ToPtr(isActive),
	}
	return s.storage.UpdateTariff(ctx, criteria, params)
}

// GetTrialTariff возвращает активный пробный тариф (бесплатный)
func (s *Service) GetTrialTariff(ctx context.Context) (*Tariff, error) {
	criteria := ListCriteria{
		IsActive: lo.ToPtr(true),
		Limit:    100,
	}
	allTariffs, err := s.storage.ListTariffs(ctx, criteria)
	if err != nil {
		return nil, err
	}

	// Ищем бесплатный тариф
	for _, t := range allTariffs {
		if t.Price == 0 {
			return t, nil
		}
	}

	return nil, nil
}
