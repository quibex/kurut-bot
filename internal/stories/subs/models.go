package subs

import "time"

type Status string

const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusExpired  Status = "expired"
	StatusDisabled Status = "disabled"
)

type Subscription struct {
	ID            int64
	UserID        int64
	TariffID      int64
	MarzbanUserID string
	MarzbanLink   string
	Status        Status
	ActivatedAt   *time.Time
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Критерии для получения подписки
type GetCriteria struct {
	IDs            []int64
	UserIDs        []int64
	MarzbanUserIDs []string
}

// Критерии для удаления подписки
type DeleteCriteria struct {
	IDs            []int64
	UserIDs        []int64
	MarzbanUserIDs []string
}

// Критерии для списка подписок
type ListCriteria struct {
	UserIDs  []int64
	TariffIDs []int64
	Status []Status
	Limit    int
	Offset   int
}

// Параметры для обновления подписки
type UpdateParams struct {
	Status      *Status
	ActivatedAt *time.Time
	ExpiresAt   *time.Time
}

// Запрос для создания множественных подписок
type CreateSubscriptionsRequest struct {
	UserID    int64
	TariffID  int64
	Quantity  int
	PaymentID *int64
}
