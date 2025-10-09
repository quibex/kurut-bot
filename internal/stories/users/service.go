package users

import (
	"context"
)

// Service provides business logic for user operations
type Service struct {
	storage Storage
}

// NewService creates a new user service
func NewService(storage Storage) *Service {
	return &Service{
		storage: storage,
	}
}

// GetOrCreateUserByTelegramID получает пользователя по Telegram ID или создает нового
func (s *Service) GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (*User, error) {
	// Сначала пытаемся найти существующего пользователя
	existingUser, err := s.storage.GetUser(ctx, GetCriteria{
		TelegramID: &telegramID,
	})
	if err != nil {
		return nil, err
	}

	// Если пользователь найден, возвращаем его
	if existingUser != nil {
		return existingUser, nil
	}

	// Если пользователь не найден, создаем нового
	newUser := User{
		TelegramID: telegramID,
	}

	createdUser, err := s.storage.CreateUser(ctx, newUser)
	if err != nil {
		return nil, err
	}

	return createdUser, nil
}

// MarkTrialAsUsed отмечает что пользователь использовал пробный период
func (s *Service) MarkTrialAsUsed(ctx context.Context, userID int64) error {
	_, err := s.storage.UpdateUser(ctx, GetCriteria{
		ID: &userID,
	}, UpdateParams{
		UsedTrial: boolPtr(true),
	})
	return err
}

func boolPtr(b bool) *bool {
	return &b
}
