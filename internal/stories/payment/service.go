package payment

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	yoopayment "github.com/rvinnie/yookassa-sdk-go/yookassa/payment"
)

// Service provides business logic for payment operations
type Service struct {
	storage        Storage
	yookassaClient YooKassaClient
	logger         *slog.Logger
	returnURL      string
	mockPayment    bool
}

// NewService creates a new payment service
func NewService(storage Storage, yookassaClient YooKassaClient, returnURL string, mockPayment bool, logger *slog.Logger) *Service {
	return &Service{
		storage:        storage,
		yookassaClient: yookassaClient,
		logger:         logger,
		returnURL:      returnURL,
		mockPayment:    mockPayment,
	}
}

// CreatePayment creates a new payment and processes it with YooKassa
func (s *Service) CreatePayment(ctx context.Context, paymentEntity Payment) (*Payment, error) {
	s.logger.Info("Creating payment",
		"user_id", paymentEntity.UserID,
		"amount", paymentEntity.Amount,
		"mock_mode", s.mockPayment,
	)

	// 1. Валидация входных данных
	if paymentEntity.Amount <= 0 {
		s.logger.Error("Invalid amount", "amount", paymentEntity.Amount)
		return nil, fmt.Errorf("amount must be positive")
	}
	if paymentEntity.UserID <= 0 {
		s.logger.Error("Invalid userID", "user_id", paymentEntity.UserID)
		return nil, fmt.Errorf("userID must be positive")
	}

	// Mock payment mode - создаём платёж сразу со статусом approved без YooKassa
	if s.mockPayment {
		return s.createMockPayment(ctx, paymentEntity)
	}

	// 2. Создаем запись в БД со статусом pending
	paymentEntity.Status = StatusPending
	createdPayment, err := s.storage.CreatePayment(ctx, paymentEntity)
	if err != nil {
		s.logger.Error("Failed to create payment in storage", "error", err, "user_id", paymentEntity.UserID)
		return nil, fmt.Errorf("failed to create payment in storage: %w", err)
	}

	// 3. Подготавливаем данные для YooKassa
	metadata := map[string]string{
		"internal_payment_id": fmt.Sprintf("%d", createdPayment.ID),
	}
	description := fmt.Sprintf("Оплата подписки #%d", createdPayment.ID)

	// 4. Вызываем YooKassa API
	s.logger.Info("Calling YooKassa API", "payment_id", createdPayment.ID, "amount", createdPayment.Amount)

	yookassaPayment, err := s.yookassaClient.CreatePayment(ctx, createdPayment.Amount, description, metadata)
	if err != nil {
		s.logger.Error("Failed to create payment in YooKassa",
			"error", err,
			"payment_id", createdPayment.ID,
			"amount", createdPayment.Amount,
		)
		return nil, fmt.Errorf("failed to create payment in YooKassa: %w", err)
	}

	s.logger.Info("Payment created in YooKassa",
		"payment_id", createdPayment.ID,
		"yookassa_id", yookassaPayment.ID,
		"status", yookassaPayment.Status,
	)

	// 5. Обновляем запись в БД с данными от YooKassa
	updateParams := UpdateParams{
		YooKassaID: &yookassaPayment.ID,
	}

	// Извлекаем payment_url из confirmation если есть
	if confirmationURL := extractPaymentURL(yookassaPayment); confirmationURL != "" {
		updateParams.PaymentURL = &confirmationURL
		s.logger.Info("Extracted payment URL", "payment_id", createdPayment.ID, "url", confirmationURL)
	} else {
		s.logger.Warn("No payment URL in YooKassa response", "payment_id", createdPayment.ID)
	}

	criteria := GetCriteria{ID: &createdPayment.ID}
	updatedPayment, err := s.storage.UpdatePayment(ctx, criteria, updateParams)
	if err != nil {
		s.logger.Error("Failed to update payment with YooKassa data",
			"error", err,
			"payment_id", createdPayment.ID,
			"yookassa_id", yookassaPayment.ID,
		)
		return nil, fmt.Errorf("failed to update payment with YooKassa data: %w", err)
	}

	s.logger.Info("Payment successfully created and updated",
		"payment_id", updatedPayment.ID,
		"yookassa_id", *updatedPayment.YooKassaID,
	)

	return updatedPayment, nil
}

// createMockPayment creates a payment with approved status without calling YooKassa
func (s *Service) createMockPayment(ctx context.Context, paymentEntity Payment) (*Payment, error) {
	now := time.Now()
	paymentEntity.Status = StatusApproved
	paymentEntity.ProcessedAt = &now

	createdPayment, err := s.storage.CreatePayment(ctx, paymentEntity)
	if err != nil {
		s.logger.Error("Failed to create mock payment in storage", "error", err, "user_id", paymentEntity.UserID)
		return nil, fmt.Errorf("failed to create mock payment in storage: %w", err)
	}

	s.logger.Info("Mock payment created with approved status",
		"payment_id", createdPayment.ID,
		"amount", createdPayment.Amount,
	)

	return createdPayment, nil
}

// CheckPaymentStatus checks payment status in YooKassa and updates local storage
func (s *Service) CheckPaymentStatus(ctx context.Context, paymentID int64) (*Payment, error) {
	s.logger.Info("Checking payment status", "payment_id", paymentID)

	// 1. Получаем платеж из БД
	criteria := GetCriteria{ID: &paymentID}
	payment, err := s.storage.GetPayment(ctx, criteria)
	if err != nil {
		s.logger.Error("Failed to get payment from storage", "error", err, "payment_id", paymentID)
		return nil, fmt.Errorf("failed to get payment from storage: %w", err)
	}
	if payment == nil {
		s.logger.Error("Payment not found", "payment_id", paymentID)
		return nil, fmt.Errorf("payment not found: %d", paymentID)
	}

	if s.mockPayment {
		s.logger.Info("Mock payment mode enabled, returning approved status", "payment_id", paymentID)
		if payment.Status != StatusApproved {
			newStatus := StatusApproved
			now := time.Now()
			updateParams := UpdateParams{
				Status:      &newStatus,
				ProcessedAt: &now,
			}
			updatedPayment, err := s.storage.UpdatePayment(ctx, criteria, updateParams)
			if err != nil {
				s.logger.Error("Failed to update payment status in mock mode",
					"error", err,
					"payment_id", paymentID,
				)
				return nil, fmt.Errorf("failed to update payment status: %w", err)
			}
			return updatedPayment, nil
		}
		return payment, nil
	}

	// 2. Проверяем что есть YooKassaID
	if payment.YooKassaID == nil {
		s.logger.Error("Payment has no YooKassaID", "payment_id", paymentID)
		return nil, fmt.Errorf("payment %d has no YooKassaID", paymentID)
	}

	// 3. Проверяем статус в YooKassa
	s.logger.Info("Checking status in YooKassa",
		"payment_id", paymentID,
		"yookassa_id", *payment.YooKassaID,
	)
	yookassaPayment, err := s.yookassaClient.GetPaymentStatus(ctx, *payment.YooKassaID)
	if err != nil {
		s.logger.Error("Failed to get payment status from YooKassa",
			"error", err,
			"payment_id", paymentID,
			"yookassa_id", *payment.YooKassaID,
		)
		return nil, fmt.Errorf("failed to get payment status from YooKassa: %w", err)
	}

	s.logger.Info("Got payment status from YooKassa",
		"payment_id", paymentID,
		"yookassa_status", yookassaPayment.Status,
		"current_status", payment.Status,
	)

	// 4. Маппим статус из YooKassa в наш внутренний статус
	newStatus := mapYooKassaStatusToInternal(yookassaPayment.Status)

	// 5. Обновляем статус в БД если изменился
	if newStatus != payment.Status {
		s.logger.Info("Payment status changed",
			"payment_id", paymentID,
			"old_status", payment.Status,
			"new_status", newStatus,
		)

		updateParams := UpdateParams{
			Status: &newStatus,
		}

		// Если платеж стал успешным, добавляем дату обработки
		if newStatus == StatusApproved {
			now := time.Now()
			updateParams.ProcessedAt = &now
			s.logger.Info("Payment approved", "payment_id", paymentID)
		}

		updatedPayment, err := s.storage.UpdatePayment(ctx, criteria, updateParams)
		if err != nil {
			s.logger.Error("Failed to update payment status",
				"error", err,
				"payment_id", paymentID,
				"new_status", newStatus,
			)
			return nil, fmt.Errorf("failed to update payment status: %w", err)
		}

		s.logger.Info("Payment status updated successfully",
			"payment_id", paymentID,
			"status", newStatus,
		)

		return updatedPayment, nil
	}

	s.logger.Info("Payment status unchanged", "payment_id", paymentID, "status", payment.Status)
	return payment, nil
}

// IsMockPayment returns true if mock payment mode is enabled
func (s *Service) IsMockPayment() bool {
	return s.mockPayment
}

// LinkPaymentToSubscriptions creates links between payment and subscriptions
func (s *Service) LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error {
	s.logger.Info("Linking payment to subscriptions",
		"payment_id", paymentID,
		"subscription_ids", subscriptionIDs,
		"count", len(subscriptionIDs),
	)

	err := s.storage.LinkPaymentToSubscriptions(ctx, paymentID, subscriptionIDs)
	if err != nil {
		s.logger.Error("Failed to link payment to subscriptions",
			"error", err,
			"payment_id", paymentID,
			"subscription_ids", subscriptionIDs,
		)
		return err
	}

	s.logger.Info("Successfully linked payment to subscriptions",
		"payment_id", paymentID,
		"count", len(subscriptionIDs),
	)
	return nil
}

// Helper functions

// extractPaymentURL извлекает URL для оплаты из YooKassa confirmation
func extractPaymentURL(payment *yoopayment.Payment) string {
	if payment.Confirmation == nil {
		return ""
	}

	// SDK использует interface{} для Confirmation, нужно type assertion
	if redirect, ok := payment.Confirmation.(*yoopayment.Redirect); ok {
		return redirect.ConfirmationURL
	}

	// Альтернативный способ через map (SDK иногда возвращает map)
	if confMap, ok := payment.Confirmation.(map[string]interface{}); ok {
		if url, exists := confMap["confirmation_url"].(string); exists {
			return url
		}
	}

	return ""
}

// mapYooKassaStatusToInternal maps YooKassa payment status to our internal status
func mapYooKassaStatusToInternal(yookassaStatus yoopayment.Status) Status {
	switch yookassaStatus {
	case yoopayment.Pending, yoopayment.WaitingForCapture:
		return StatusPending
	case yoopayment.Succeeded:
		return StatusApproved
	case yoopayment.Canceled:
		return StatusCancelled
	default:
		return StatusPending
	}
}
