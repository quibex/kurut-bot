package orders

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

type PendingOrder struct {
	ID                     int64
	PaymentID              int64
	AdminUserID            int64
	AssistantTelegramID    int64
	ChatID                 int64
	MessageID              *int
	ClientWhatsApp         string
	ServerID               *int64  // ID выбранного сервера
	ServerName             *string // Название сервера
	IsMigrationFlag        bool    // true только для миграционных заказов
	TariffID               int64
	TariffName             string
	TotalAmount            float64
	ReferrerWhatsApp       *string // WhatsApp of referrer (who invited)
	ReferrerSubscriptionID *int64  // ID of referrer's subscription to extend
	ReferralType           *string // 'referral' or 'partnership'
	Status                 Status
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// IsMigration returns true if this is a migration order
func (p *PendingOrder) IsMigration() bool {
	return p.IsMigrationFlag
}
