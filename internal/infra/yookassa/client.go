package yookassa

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/rvinnie/yookassa-sdk-go/yookassa"
	yoocommon "github.com/rvinnie/yookassa-sdk-go/yookassa/common"
	yoopayment "github.com/rvinnie/yookassa-sdk-go/yookassa/payment"
)

// Client wraps the YooKassa SDK client
type Client struct {
	client    *yookassa.Client
	logger    *slog.Logger
	returnURL string
}

// NewClient creates a new YooKassa client wrapper
func NewClient(shopID, secretKey, returnURL string, logger *slog.Logger) (*Client, error) {
	client := yookassa.NewClient(shopID, secretKey)

	return &Client{
		client:    client,
		logger:    logger,
		returnURL: returnURL,
	}, nil
}

// CreatePayment creates a new payment in YooKassa
func (c *Client) CreatePayment(ctx context.Context, amount float64, description string, metadata map[string]string) (*yoopayment.Payment, error) {
	c.logger.Info("Creating payment in YooKassa", "amount", amount)

	// Создаём идемпотентность ключ
	idempotenceKey := fmt.Sprintf("%s_%d", uuid.New().String(), time.Now().Unix())

	// Создаём запрос на создание платежа
	payment := &yoopayment.Payment{
		Amount: &yoocommon.Amount{
			Value:    fmt.Sprintf("%.2f", amount),
			Currency: "RUB",
		},
		Confirmation: &yoopayment.Redirect{
			Type:      yoopayment.TypeRedirect,
			ReturnURL: c.returnURL,
		},
		Description: description,
		Metadata:    metadata,
		Capture:     true, // Автоматическое подтверждение платежа
	}

	paymentHandler := yookassa.NewPaymentHandler(c.client).WithIdempotencyKey(idempotenceKey)
	result, err := paymentHandler.CreatePayment(payment)
	if err != nil {
		c.logger.Error("Failed to create payment in YooKassa", "error", err)
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	c.logger.Info("Payment created successfully in YooKassa", "payment_id", result.ID, "status", result.Status)
	return result, nil
}

// GetPaymentStatus gets payment status from YooKassa
func (c *Client) GetPaymentStatus(ctx context.Context, paymentID string) (*yoopayment.Payment, error) {
	c.logger.Info("Getting payment status from YooKassa", "payment_id", paymentID)

	paymentHandler := yookassa.NewPaymentHandler(c.client)
	result, err := paymentHandler.FindPayment(paymentID)
	if err != nil {
		c.logger.Error("Failed to get payment status", "error", err, "payment_id", paymentID)
		return nil, fmt.Errorf("failed to get payment status: %w", err)
	}

	c.logger.Info("Payment status retrieved", "payment_id", paymentID, "status", result.Status)
	return result, nil
}
