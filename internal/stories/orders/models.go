package orders

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

type PendingOrder struct {
	ID                  int64
	PaymentID           int64
	AdminUserID         int64
	AssistantTelegramID int64
	ChatID              int64
	MessageID           *int
	ClientWhatsApp      string
	TariffID            int64
	TariffName          string
	TotalAmount         float64
	Status              Status
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
