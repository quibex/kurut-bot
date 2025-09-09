package tariffs

import "time"

type Tariff struct {
	ID             int64
	Name           string
	DurationDays   int
	Price          float64
	TrafficLimitGB *int
	IsActive     bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Критерии для получения тарифа
type GetCriteria struct {
	ID *int64
}

// Критерии для удаления тарифа
type DeleteCriteria struct {
	ID *int64
}

// Критерии для списка тарифов
type ListCriteria struct {
	IsActive *bool
	Limit      int
	Offset     int
}

// Параметры для обновления тарифа
type UpdateParams struct {
	Name           *string
	DurationDays   *int
	Price          *float64
	TrafficLimitGB *int
	IsActive     *bool
}
