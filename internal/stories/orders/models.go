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
	ServerID            *int64  // Если заполнен - это миграция (сервер выбран вручную)
	ServerName          *string // Название сервера для миграции
	TariffID            int64
	TariffName          string
	TotalAmount         float64
	Status              Status
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// IsMigration returns true if this is a migration order (server was manually selected)
func (p *PendingOrder) IsMigration() bool {
	return p.ServerID != nil
}
