package payment

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

type Payment struct {
	ID                    int64
	UserID              int64
	Amount                float64
	Status                Status
	CardlinkTransactionID *string
	PaymentURL            *string
	ProcessedAt           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// Критерии для получения платежа
type GetCriteria struct {
	ID                    *int64
	CardlinkTransactionID *string
}

// Критерии для удаления платежа
type DeleteCriteria struct {
	ID                    *int64
	CardlinkTransactionID *string
}

// Критерии для списка платежей
type ListCriteria struct {
	UserID *int64
	Status *Status
	Limit  int
	Offset int
}

// Параметры для обновления платежа
type UpdateParams struct {
	Status                *Status
	CardlinkTransactionID *string
	ProcessedAt           *time.Time
}

// Дополнительная информация для создания платежа (для внешних систем)
type CreatePaymentMeta struct {
	OrderID     string // Идентификатор заказа для cardlink
	Description string // Описание платежа
}

type PaymentSubscription struct {
	PaymentID      int64
	SubscriptionID int64
}

type CreatePaymentSubscriptionRequest struct {
	PaymentID      int64
	SubscriptionID int64
}
