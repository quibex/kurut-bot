package users

import "time"

type User struct {
	ID         int64
	TelegramID int64
	UsedTrial  bool
	Language   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Критерии для получения пользователя
type GetCriteria struct {
	ID         *int64
	TelegramID *int64
}

// Критерии для удаления пользователя
type DeleteCriteria struct {
	ID         *int64
	TelegramID *int64
}

// Критерии для списка пользователей
type ListCriteria struct {
	Limit  int
	Offset int
}

// Параметры для обновления пользователя
type UpdateParams struct {
	UsedTrial *bool
	Language  *string
}
