package subs

import (
	"fmt"
	"regexp"
	"time"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusExpired  Status = "expired"
	StatusDisabled Status = "disabled"
)

type Subscription struct {
	ID                  int64
	UserID              int64
	TariffID            int64
	ServerID            *int64
	Status              Status
	ClientWhatsApp      *string
	GeneratedUserID     *string
	CreatedByTelegramID *int64
	ReferrerWhatsApp    *string // WhatsApp of the person who invited this client
	ActivatedAt         *time.Time
	ExpiresAt           *time.Time
	LastRenewedAt       *time.Time
	RenewalCount        int // Number of times this subscription has been renewed
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Критерии для получения подписки
type GetCriteria struct {
	IDs     []int64
	UserIDs []int64
}

// Критерии для удаления подписки
type DeleteCriteria struct {
	IDs     []int64
	UserIDs []int64
}

// Критерии для списка подписок
type ListCriteria struct {
	UserIDs             []int64
	TariffIDs           []int64
	Status              []Status
	CreatedByTelegramID *int64
	Limit               int
	Offset              int
}

// Параметры для обновления подписки
type UpdateParams struct {
	Status      *Status
	ActivatedAt *time.Time
	ExpiresAt   *time.Time
}

// Запрос для создания подписки
type CreateSubscriptionRequest struct {
	UserID                 int64
	TariffID               int64
	PaymentID              *int64
	ClientWhatsApp         string
	CreatedByTelegramID    int64
	ReferrerSubscriptionID *int64 // ID of referrer's subscription to extend with bonus
}

// Запрос для миграции существующего клиента (без увеличения счётчика сервера)
type MigrateSubscriptionRequest struct {
	UserID              int64
	TariffID            int64
	ServerID            int64 // Конкретный сервер (выбирается вручную)
	ClientWhatsApp      string
	CreatedByTelegramID int64
}

// Результат создания подписки
type CreateSubscriptionResult struct {
	Subscription         *Subscription
	GeneratedUserID      string
	ServerUIURL          *string
	ServerUIPassword     *string
	ReferralBonusApplied bool       // true if referral bonus was applied
	ReferrerWhatsApp     *string    // referrer's WhatsApp number
	ReferrerNewExpiresAt *time.Time // referrer's new expiration date after bonus
	ReferrerWeeklyCount  int        // how many people this referrer invited this week
}

// GenerateUserID создает уникальный идентификатор пользователя для VPN
// Формат: {subscription_id}_{last 3 digits of assistant_telegram_id}_{last 4 digits of client_phone}
// Пример: 10_881_3456
func GenerateUserID(subscriptionID int64, assistantTelegramID int64, clientWhatsApp string) string {
	// Получаем последние 3 цифры telegram ID ассистента
	tgIDStr := fmt.Sprintf("%d", assistantTelegramID)
	tgSuffix := tgIDStr
	if len(tgIDStr) > 3 {
		tgSuffix = tgIDStr[len(tgIDStr)-3:]
	}

	// Извлекаем только цифры из номера телефона
	re := regexp.MustCompile(`\d`)
	digits := re.FindAllString(clientWhatsApp, -1)
	phoneDigits := ""
	for _, d := range digits {
		phoneDigits += d
	}

	// Получаем последние 4 цифры номера телефона
	phoneSuffix := phoneDigits
	if len(phoneDigits) > 4 {
		phoneSuffix = phoneDigits[len(phoneDigits)-4:]
	}

	return fmt.Sprintf("%d_%s_%s", subscriptionID, tgSuffix, phoneSuffix)
}
