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
	ID          int64
	UserID      int64
	Amount      float64
	Status      Status
	YooKassaID  *string
	PaymentURL  *string
	ProcessedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GetCriteria struct {
	ID         *int64
	YooKassaID *string
}

type DeleteCriteria struct {
	ID *int64
}

// Критерии для списка платежей
type ListCriteria struct {
	UserID *int64
	Status *Status
	Limit  int
	Offset int
}

type UpdateParams struct {
	Status      *Status
	YooKassaID  *string
	PaymentURL  *string
	ProcessedAt *time.Time
}

type CreatePaymentMeta struct {
	Description string
}

type PaymentSubscription struct {
	PaymentID      int64
	SubscriptionID int64
}

type CreatePaymentSubscriptionRequest struct {
	PaymentID      int64
	SubscriptionID int64
}
