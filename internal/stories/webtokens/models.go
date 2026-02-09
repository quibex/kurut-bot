package webtokens

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

type PurchaseTokenStatus string

const (
	PurchaseStatusPending   PurchaseTokenStatus = "pending"
	PurchaseStatusPaid      PurchaseTokenStatus = "paid"
	PurchaseStatusCompleted PurchaseTokenStatus = "completed"
	PurchaseStatusCancelled PurchaseTokenStatus = "cancelled"
)

type PurchaseToken struct {
	ID                  int64
	Token               string
	ClientWhatsApp      string
	CreatedByTelegramID int64
	TariffID            *int64
	ReferrerWhatsApp    *string
	PaymentID           *int64
	Status              PurchaseTokenStatus
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type RenewalToken struct {
	ID             int64
	Token          string
	SubscriptionID int64
	CreatedAt      time.Time
}

// ClientToken — один токен на клиента (WhatsApp), используется для веб-страницы
type ClientToken struct {
	ID                  int64
	Token               string
	WhatsApp            string
	PartnerWhatsApp     *string // WhatsApp партнера, если клиент пришел от партнера
	CreatedByTelegramID int64
	CreatedAt           time.Time
}

// GenerateToken creates a cryptographically secure random token (32 bytes -> base64url encoded)
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
