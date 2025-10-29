package marzban

import "time"

const (
	ProtocolVLESS  = "vless"
	ProtocolTrojan = "trojan"
)

const (
	InboundVLESSDefault  = "VLESS TCP REALITY"
	InboundTrojanDefault = "TROJAN TCP NOTLS"
)

type CreateUserRequest struct {
	Username       string
	Protocols      []string
	ExpiresAt      time.Time
	TrafficLimitGB *int
}

type UserSubscription struct {
	MarzbanUserID   string
	SubscriptionURL string
}

type ProtocolConfig struct {
	Name     string
	Inbounds []string
}

