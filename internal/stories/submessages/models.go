package submessages

import "time"

type Type string

const (
	TypeExpiring Type = "expiring"
	TypeOverdue  Type = "overdue"
)

type SubscriptionMessage struct {
	ID               int64
	SubscriptionID   int64
	ChatID           int64
	MessageID        int
	Type             Type
	IsActive         bool
	SelectedTariffID *int64
	CreatedAt        time.Time
}
