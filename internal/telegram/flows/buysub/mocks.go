package buysub

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"kurut-bot/internal/stories/payment"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/telegram/states"
)

// MockBotApi - мок Telegram Bot API
type MockBotApi struct {
	SentMessages []tgbotapi.Chattable
}

func (m *MockBotApi) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.SentMessages = append(m.SentMessages, c)
	return tgbotapi.Message{MessageID: len(m.SentMessages)}, nil
}

func (m *MockBotApi) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	return &tgbotapi.APIResponse{Ok: true}, nil
}

// MockTariffService - мок сервиса тарифов
type MockTariffService struct{}

func (m *MockTariffService) GetActiveTariffs(ctx context.Context) ([]*tariffs.Tariff, error) {
	return []*tariffs.Tariff{
		{
			ID:           1,
			Name:         "Базовый",
			DurationDays: 30,
			Price:        199.0,
			IsActive:     true,
			CreatedAt:    time.Now(),
		},
		{
			ID:           2,
			Name:         "Стандарт",
			DurationDays: 30,
			Price:        299.0,
			IsActive:     true,
			CreatedAt:    time.Now(),
		},
		{
			ID:           3,
			Name:         "Премиум",
			DurationDays: 90,
			Price:        799.0,
			IsActive:     true,
			CreatedAt:    time.Now(),
		},
	}, nil
}

func (m *MockTariffService) CreateTariff(ctx context.Context, tariff tariffs.Tariff) (*tariffs.Tariff, error) {
	// Mock implementation - just return the tariff with a generated ID
	createdTariff := &tariffs.Tariff{
		ID:             int64(time.Now().Unix()), // Simple ID generation for mock
		Name:           tariff.Name,
		DurationDays:   tariff.DurationDays,
		Price:          tariff.Price,
		TrafficLimitGB: tariff.TrafficLimitGB,
		IsActive:       true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	return createdTariff, nil
}

// MockSubscriptionService - мок сервиса подписок
type MockSubscriptionService struct {
	NextID        int64
	Subscriptions []subs.Subscription
}

func NewMockSubscriptionService() *MockSubscriptionService {
	return &MockSubscriptionService{
		NextID:        1,
		Subscriptions: make([]subs.Subscription, 0),
	}
}

func (m *MockSubscriptionService) CreateSubscriptions(ctx context.Context, req *subs.CreateSubscriptionsRequest) ([]subs.Subscription, error) {
	var result []subs.Subscription

	for i := 0; i < req.Quantity; i++ {
		marzbanUserID := fmt.Sprintf("kurut_user_%d_%d", req.UserID, m.NextID)
		marzbanLink := fmt.Sprintf("vmess://eyJ2IjoiMiIsInBzIjoiS3VydXQgVlBOICMlZCIsImFkZCI6InZwbi5rdXJ1dC5jb20iLCJwb3J0Ijo0NDMsImlkIjoidXNlcl8lZF8lZCIsImFpZCI6MCwic2N5IjoiYXV0byIsIm5ldCI6IndzIiwidHlwZSI6Im5vbmUiLCJob3N0IjoidnBuLmt1cnV0LmNvbSIsInBhdGgiOiIvcGF0aCIsInRscyI6InRscyIsInNuaSI6InZwbi5rdXJ1dC5jb20ifQ==%d_%d", req.UserID, m.NextID)

		subscription := subs.Subscription{
			ID:            m.NextID,
			UserID:        req.UserID,
			TariffID:      req.TariffID,
			MarzbanUserID: marzbanUserID,
			MarzbanLink:   marzbanLink,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		m.Subscriptions = append(m.Subscriptions, subscription)
		result = append(result, subscription)
		m.NextID++
	}

	return result, nil
}

func (m *MockSubscriptionService) GetUserSubscriptions(ctx context.Context, userID int64) ([]subs.Subscription, error) {
	var result []subs.Subscription
	for _, sub := range m.Subscriptions {
		if sub.UserID == userID {
			result = append(result, sub)
		}
	}
	return result, nil
}

// MockPaymentService - мок сервиса платежей
type MockPaymentService struct {
	NextID   int64
	Payments []*payment.Payment
}

func NewMockPaymentService() *MockPaymentService {
	return &MockPaymentService{
		NextID:   1,
		Payments: make([]*payment.Payment, 0),
	}
}

func (m *MockPaymentService) CreatePayment(ctx context.Context, paymentEntity payment.Payment) (*payment.Payment, error) {
	// Генерируем фейковую ссылку на оплату
	paymentURL := fmt.Sprintf("https://demo.cardlink.com/pay/%d?amount=%.2f",
		m.NextID, paymentEntity.Amount)

	pmt := &payment.Payment{
		ID:         m.NextID,
		UserID:     paymentEntity.UserID,
		Amount:     paymentEntity.Amount,
		Status:     paymentEntity.Status,
		PaymentURL: &paymentURL,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	m.Payments = append(m.Payments, pmt)
	currentID := m.NextID
	m.NextID++

	// Симулируем успешную оплату через 5 секунд
	go func() {
		time.Sleep(5 * time.Second)

		// Обновляем статус платежа
		for i := range m.Payments {
			if m.Payments[i].ID == currentID {
				m.Payments[i].Status = payment.StatusApproved
				cardlinkTxID := fmt.Sprintf("mock_tx_%d_%d", currentID, time.Now().Unix())
				m.Payments[i].CardlinkTransactionID = &cardlinkTxID
				processedAt := time.Now()
				m.Payments[i].ProcessedAt = &processedAt
				break
			}
		}
	}()

	return pmt, nil
}

func (m *MockPaymentService) LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error {
	// В моке просто возвращаем успех
	return nil
}

func (m *MockPaymentService) ProcessPaymentSuccess(ctx context.Context, paymentID int64, cardlinkTransactionID string) error {
	for i := range m.Payments {
		if m.Payments[i].ID == paymentID {
			m.Payments[i].Status = payment.StatusApproved
			m.Payments[i].CardlinkTransactionID = &cardlinkTransactionID
			processedAt := time.Now()
			m.Payments[i].ProcessedAt = &processedAt
			break
		}
	}
	return nil
}

func (m *MockPaymentService) GetPaymentByID(ctx context.Context, paymentID int64) (*payment.Payment, error) {
	for _, p := range m.Payments {
		if p.ID == paymentID {
			return p, nil
		}
	}
	return nil, fmt.Errorf("payment not found: %d", paymentID)
}

// NewMockHandler создает новый Handler с моками для тестирования
func NewMockHandler() *Handler {
	return NewHandler(
		&MockBotApi{SentMessages: make([]tgbotapi.Chattable, 0)},
		states.NewManager(), // Используем реальный менеджер вместо мока
		&MockTariffService{},
		NewMockSubscriptionService(),
		NewMockPaymentService(),
		slog.Default(),
	)
}
