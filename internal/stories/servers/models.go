package servers

import "time"

type Server struct {
	ID           int64
	Name         string
	UIURL        string
	UIPassword   string
	CurrentUsers int
	MaxUsers     int
	Archived     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// GetCriteria - критерии для получения сервера
type GetCriteria struct {
	ID       *int64
	Archived *bool
}

// ListCriteria - критерии для списка серверов
type ListCriteria struct {
	Archived *bool
	Limit    int
	Offset   int
}

// UpdateParams - параметры для обновления сервера
type UpdateParams struct {
	Name         *string
	UIURL        *string
	UIPassword   *string
	CurrentUsers *int
	MaxUsers     *int
	Archived     *bool
}
