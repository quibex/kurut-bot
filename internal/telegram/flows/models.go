package flows

import "time"

// BuySubFlowData - data for buy sub
type BuySubFlowData struct {
	UserID      int64 // Внутренний ID пользователя
	TariffID    int64
	TariffName  string
	Price       float64
	TotalAmount float64
	PaymentID   *int64
	PaymentURL  *string
	MessageID   *int // ID сообщения для бесшовного редактирования
}

// CreateSubFlowData - data for create sub
type CreateSubFlowData struct {
	UserName   string
	TariffName string
}

// CreateSubForClientFlowData - data for assistant creating sub for client
type CreateSubForClientFlowData struct {
	AdminUserID            int64
	AssistantTelegramID    int64
	ClientWhatsApp         string
	ReferrerWhatsApp       *string // WhatsApp of the referrer (who invited)
	ReferrerSubscriptionID *int64  // ID of referrer's active subscription to extend
	IsPartnership          bool    // true if referrer is a partner (no bonus, no subscription check)
	TariffID               int64
	TariffName             string
	Price                  float64
	TotalAmount            float64
	ServerID               *int64  // ID выбранного сервера (nil = авто-выбор)
	ServerName             *string // Название выбранного сервера
	PaymentID              *int64
	PaymentURL             *string
	MessageID              *int // ID сообщения для бесшовного редактирования
	IsTrialEligible        bool // true if client can get trial
}

// DisableSubFlowData - data for disable sub
type DisableSubFlowData struct {
	UserName string
}

// EnableSubFlowData - data for enable sub
type EnableSubFlowData struct {
	UserName string
}

// CreateTariffFlowData - data for create tariff
type CreateTariffFlowData struct {
	Name           string
	Price          float64
	DurationDays   int
	TrafficLimitGB *int // опционально
}

// RenewSubFlowData - data for renew sub
type RenewSubFlowData struct {
	UserID         int64
	SubscriptionID int64
	TariffID       int64
	TariffName     string
	DurationDays   int
	Price          float64
	PaymentID      *int64
	PaymentURL     *string
	ClientName     *string // Имя клиента, если это клиентская подписка
	Page           int     // Current page for subscription list
	MessageID      *int    // ID сообщения для бесшовного редактирования
}

// WelcomeFlowData - data for welcome/start flow
type WelcomeFlowData struct {
	MessageID int
}

// AddServerFlowData - data for adding server
type AddServerFlowData struct {
	Name         string
	UIURL        string
	UIPassword   string
	CurrentUsers int
	MaxUsers     int
}

// MigrateClientFlowData - data for migrating existing client
type MigrateClientFlowData struct {
	AdminUserID         int64
	AssistantTelegramID int64
	ClientWhatsApp      string
	ServerID            int64
	ServerName          string
	MessageID           *int
}

// NewClientFlowData - data for creating new client link
type NewClientFlowData struct {
	AssistantTelegramID int64
	ClientWhatsApp      string
	PartnerWhatsApp     *string
	ServerID            int64
	ServerName          string
	MessageID           *int
}

// LookupSubResult holds subscription data for the lookup flow
type LookupSubResult struct {
	ID             int64
	ClientWhatsApp *string
	TariffName     string
	Status         string
	ExpiresAt      *time.Time
	LastRenewedAt  *time.Time
	CreatedAt      time.Time
	ServerID       *int64
	ServerName     *string
}

// LookupFlowData - data for subscription lookup flow with navigation
type LookupFlowData struct {
	Results     []LookupSubResult
	CurrentIdx  int
	MessageID   *int
	Suffix      string
	ClientLinks map[string]string
}
