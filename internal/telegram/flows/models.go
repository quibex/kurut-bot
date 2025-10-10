package flows

// BuySubFlowData - data for buy sub
type BuySubFlowData struct {
	UserID      int64 // Внутренний ID пользователя
	TariffID    int64
	TariffName  string
	Price       float64
	TotalAmount float64
	PaymentID   *int64
	PaymentURL  *string
	Language    string
}

// CreateSubFlowData - data for create sub
type CreateSubFlowData struct {
	UserName   string
	TariffName string
}

// CreateSubForClientFlowData - data for admin creating sub for client
type CreateSubForClientFlowData struct {
	AdminUserID int64
	ClientName  string
	TariffID    int64
	TariffName  string
	Price       float64
	TotalAmount float64
	PaymentID   *int64
	PaymentURL  *string
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
	Language       string
}
