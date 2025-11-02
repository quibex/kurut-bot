package subs

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusExpired  Status = "expired"
	StatusDisabled Status = "disabled"
)

type VPNType string

const (
	VPNTypeMarzban   VPNType = "marzban"
	VPNTypeWireGuard VPNType = "wireguard"
)

type Subscription struct {
	ID            int64
	UserID        int64
	TariffID      int64
	MarzbanUserID string
	MarzbanLink   string
	VPNType       string
	VPNData       *string
	Status        Status
	ClientName    *string
	ActivatedAt   *time.Time
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type WireGuardData struct {
	ServerID  int64  `json:"server_id"`
	PublicKey string `json:"public_key"`
	AllowedIP string `json:"allowed_ip"`
	Config    string `json:"config"`
	QRCode    string `json:"qr_code"`
}

func (s *Subscription) GetWireGuardData() (*WireGuardData, error) {
	if s.VPNData == nil || *s.VPNData == "" {
		return nil, nil
	}

	var data WireGuardData
	if err := json.Unmarshal([]byte(*s.VPNData), &data); err != nil {
		return nil, err
	}

	return &data, nil
}

func MarshalWireGuardData(data WireGuardData) (*string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	str := string(bytes)
	return &str, nil
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
	UserIDs   []int64
	TariffIDs []int64
	Status    []Status
	Limit     int
	Offset    int
}

// Параметры для обновления подписки
type UpdateParams struct {
	Status      *Status
	ActivatedAt *time.Time
	ExpiresAt   *time.Time
}

// Запрос для создания подписки
type CreateSubscriptionRequest struct {
	UserID     int64
	TariffID   int64
	PaymentID  *int64
	ClientName *string
}
