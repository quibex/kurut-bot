package payment

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	yoopayment "github.com/rvinnie/yookassa-sdk-go/yookassa/payment"
)

// moscowLocation is Moscow timezone (UTC+3)
var moscowLocation = time.FixedZone("MSK", 3*60*60)

// Service provides business logic for payment operations
type Service struct {
	storage        Storage
	yookassaClient YooKassaClient
	logger         *slog.Logger
	returnURL      string
	manualPayment  bool
}

// NewService creates a new payment service
func NewService(storage Storage, yookassaClient YooKassaClient, returnURL string, manualPayment bool, logger *slog.Logger) *Service {
	return &Service{
		storage:        storage,
		yookassaClient: yookassaClient,
		logger:         logger,
		returnURL:      returnURL,
		manualPayment:  manualPayment,
	}
}

// CreatePayment creates a new payment and processes it with YooKassa
func (s *Service) CreatePayment(ctx context.Context, paymentEntity Payment) (*Payment, error) {
	return s.CreatePaymentWithReturnURL(ctx, paymentEntity, "")
}

// CreatePaymentWithReturnURL creates a new payment with a custom return URL
func (s *Service) CreatePaymentWithReturnURL(ctx context.Context, paymentEntity Payment, returnURL string) (*Payment, error) {
	s.logger.Info("Creating payment",
		"user_id", paymentEntity.UserID,
		"amount", paymentEntity.Amount,
		"manual_mode", s.manualPayment,
		"return_url", returnURL,
	)

	// 1. Валидация входных данных
	if paymentEntity.Amount <= 0 {
		s.logger.Warn("Invalid amount", "amount", paymentEntity.Amount)
		return nil, fmt.Errorf("amount must be positive")
	}
	if paymentEntity.UserID <= 0 {
		s.logger.Warn("Invalid userID", "user_id", paymentEntity.UserID)
		return nil, fmt.Errorf("userID must be positive")
	}

	// Manual payment mode - создаём платёж сразу со статусом approved без YooKassa
	if s.manualPayment {
		return s.createManualPayment(ctx, paymentEntity)
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

	var yookassaPayment *yoopayment.Payment
	if returnURL != "" {
		yookassaPayment, err = s.yookassaClient.CreatePaymentWithReturnURL(ctx, createdPayment.Amount, description, metadata, returnURL)
	} else {
		yookassaPayment, err = s.yookassaClient.CreatePayment(ctx, createdPayment.Amount, description, metadata)
	}
	if err != nil {
		// Upstream YooKassa failures surface via the payment-creation-failure
		// alert regex; logging as Warn keeps the generic error-rate alert
		// from firing on transient network flaps.
		s.logger.Warn("Failed to create payment in YooKassa",
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

// CancelPayment cancels a pending payment (both in DB and YooKassa).
// Returns ErrPaymentAlreadyApproved if the payment was already paid in YooKassa.
func (s *Service) CancelPayment(ctx context.Context, paymentID int64) error {
	s.logger.Info("Cancelling payment", "payment_id", paymentID)

	// First get the payment to retrieve YooKassaID
	criteria := GetCriteria{ID: &paymentID}
	p, err := s.storage.GetPayment(ctx, criteria)
	if err != nil {
		s.logger.Error("Failed to get payment for cancellation", "error", err, "payment_id", paymentID)
		return fmt.Errorf("failed to get payment: %w", err)
	}

	if p == nil {
		s.logger.Warn("Payment not found for cancellation", "payment_id", paymentID)
		return nil
	}

	// If already approved in DB, don't cancel
	if p.Status == StatusApproved {
		s.logger.Info("Payment already approved in DB, skipping cancellation", "payment_id", paymentID)
		return ErrPaymentAlreadyApproved
	}

	s.logger.Info("Found payment for cancellation",
		"payment_id", paymentID,
		"status", p.Status,
		"has_yookassa_id", p.YooKassaID != nil,
	)

	// Check actual YooKassa status before cancelling to prevent race condition
	if p.Status == StatusPending && p.YooKassaID != nil && *p.YooKassaID != "" {
		yookassaPayment, err := s.yookassaClient.GetPaymentStatus(ctx, *p.YooKassaID)
		if err != nil {
			s.logger.Warn("Failed to check YooKassa status before cancel", "error", err, "payment_id", paymentID)
			// Fall through to cancellation attempt
		} else {
			actualStatus := mapYooKassaStatusToInternal(yookassaPayment.Status)
			if actualStatus == StatusApproved {
				// Payment was already paid in YooKassa — update DB and return
				s.logger.Info("Payment already approved in YooKassa, updating DB and skipping cancellation",
					"payment_id", paymentID,
					"yookassa_id", *p.YooKassaID,
				)
				now := time.Now().In(moscowLocation)
				approvedStatus := StatusApproved
				_, _ = s.storage.UpdatePayment(ctx, criteria, UpdateParams{
					Status:      &approvedStatus,
					ProcessedAt: &now,
				})
				return ErrPaymentAlreadyApproved
			}
		}

		// Payment is still pending in YooKassa — proceed with cancellation
		s.logger.Info("Calling YooKassa to cancel payment", "yookassa_id", *p.YooKassaID)
		if err := s.yookassaClient.CancelPayment(ctx, *p.YooKassaID); err != nil {
			s.logger.Warn("Failed to cancel payment in YooKassa", "error", err, "payment_id", paymentID, "yookassa_id", *p.YooKassaID)
			// Continue to update DB status even if YooKassa cancellation fails
		} else {
			s.logger.Info("Payment cancelled in YooKassa", "payment_id", paymentID, "yookassa_id", *p.YooKassaID)
		}
	} else {
		s.logger.Info("Skipping YooKassa cancellation",
			"payment_id", paymentID,
			"status", p.Status,
			"has_yookassa_id", p.YooKassaID != nil,
		)
	}

	// Update status in DB
	cancelledStatus := StatusCancelled
	_, err = s.storage.UpdatePayment(ctx, criteria, UpdateParams{
		Status: &cancelledStatus,
	})
	if err != nil {
		s.logger.Error("Failed to cancel payment in DB", "error", err, "payment_id", paymentID)
		return fmt.Errorf("failed to cancel payment: %w", err)
	}

	s.logger.Info("Payment cancelled in DB", "payment_id", paymentID)
	return nil
}

// createManualPayment creates a payment with approved status without calling YooKassa
func (s *Service) createManualPayment(ctx context.Context, paymentEntity Payment) (*Payment, error) {
	now := time.Now().In(moscowLocation)
	paymentEntity.Status = StatusApproved
	paymentEntity.ProcessedAt = &now

	createdPayment, err := s.storage.CreatePayment(ctx, paymentEntity)
	if err != nil {
		s.logger.Error("Failed to create manual payment in storage", "error", err, "user_id", paymentEntity.UserID)
		return nil, fmt.Errorf("failed to create manual payment in storage: %w", err)
	}

	s.logger.Info("Manual payment created with approved status",
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

	if s.manualPayment {
		s.logger.Info("Manual payment mode enabled, returning approved status", "payment_id", paymentID)
		if payment.Status != StatusApproved {
			newStatus := StatusApproved
			now := time.Now().In(moscowLocation)
			updateParams := UpdateParams{
				Status:      &newStatus,
				ProcessedAt: &now,
			}
			updatedPayment, err := s.storage.UpdatePayment(ctx, criteria, updateParams)
			if err != nil {
				s.logger.Error("Failed to update payment status in manual mode",
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
			now := time.Now().In(moscowLocation)
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

// IsManualPayment returns true if manual payment mode is enabled
func (s *Service) IsManualPayment() bool {
	return s.manualPayment
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
