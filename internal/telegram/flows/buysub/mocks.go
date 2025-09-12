package buysub

import (
	"context"
	"fmt"
	"time"


	"kurut-bot/internal/stories/payment"
)

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
