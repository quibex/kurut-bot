package yookassa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rvinnie/yookassa-sdk-go/yookassa"
	yoocommon "github.com/rvinnie/yookassa-sdk-go/yookassa/common"
	yoopayment "github.com/rvinnie/yookassa-sdk-go/yookassa/payment"
	"github.com/sony/gobreaker/v2"
)

// ErrRateLimited is returned when YooKassa throttles requests (HTTP 429
// surfaces as "so many requests" in the SDK's error message).
var ErrRateLimited = errors.New("yookassa: rate limited")

// ErrUnavailable is returned when YooKassa is unreachable (network timeouts,
// DNS failures) or the circuit breaker is open. Callers can treat this as a
// transient outage and log it at WARN instead of ERROR.
var ErrUnavailable = errors.New("yookassa: unavailable")

// isTransportErr heuristically detects network/transport failures from the
// SDK's wrapped error messages (it doesn't expose typed errors).
func isTransportErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	needles := []string{
		"timeout", "timed out",
		"connection refused", "connection reset",
		"no such host", "i/o timeout", "eof",
		"network is unreachable", "broken pipe",
	}
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// classifyErr wraps known error categories so callers can react via errors.Is.
func classifyErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "so many requests") {
		return fmt.Errorf("%w: %v", ErrRateLimited, err)
	}
	if isTransportErr(err) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return err
}

// Client wraps the YooKassa SDK client
type Client struct {
	client    *yookassa.Client
	logger    *slog.Logger
	returnURL string
	breaker   *gobreaker.CircuitBreaker[*yoopayment.Payment]
}

// NewClient creates a new YooKassa client wrapper
func NewClient(shopID, secretKey, returnURL string, logger *slog.Logger) (*Client, error) {
	client := yookassa.NewClient(shopID, secretKey)

	br := gobreaker.NewCircuitBreaker[*yoopayment.Payment](gobreaker.Settings{
		Name:        "yookassa",
		MaxRequests: 1,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			// Only transport-level failures should trip the breaker;
			// rate limits and 4xx responses are not outages.
			return !isTransportErr(err)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("YooKassa circuit breaker state changed",
				"name", name, "from", from.String(), "to", to.String())
		},
	})

	return &Client{
		client:    client,
		logger:    logger,
		returnURL: returnURL,
		breaker:   br,
	}, nil
}

// wrapBreakerErr translates gobreaker-specific errors into ErrUnavailable so
// workers treat breaker-open states the same as real network outages.
func wrapBreakerErr(err error) error {
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return classifyErr(err)
}

// CreatePayment creates a new payment in YooKassa
func (c *Client) CreatePayment(ctx context.Context, amount float64, description string, metadata map[string]string) (*yoopayment.Payment, error) {
	return c.CreatePaymentWithReturnURL(ctx, amount, description, metadata, c.returnURL)
}

// CreatePaymentWithReturnURL creates a new payment in YooKassa with custom return URL
func (c *Client) CreatePaymentWithReturnURL(ctx context.Context, amount float64, description string, metadata map[string]string, returnURL string) (*yoopayment.Payment, error) {
	c.logger.Info("Creating payment in YooKassa", "amount", amount, "return_url", returnURL)

	idempotenceKey := fmt.Sprintf("%s_%d", uuid.New().String(), time.Now().Unix())

	payment := &yoopayment.Payment{
		Amount: &yoocommon.Amount{
			Value:    fmt.Sprintf("%.2f", amount),
			Currency: "RUB",
		},
		Confirmation: &yoopayment.Redirect{
			Type:      yoopayment.TypeRedirect,
			ReturnURL: returnURL,
		},
		Description: description,
		Metadata:    metadata,
		Capture:     true,
		Receipt: &yoopayment.Receipt{
			Customer: &yoocommon.Customer{
				Email: "lawalig65@gmail.com",
			},
			Items: []*yoocommon.Item{
				{
					Description: description,
					Quantity:    "1",
					Amount: &yoocommon.Amount{
						Value:    fmt.Sprintf("%.2f", amount),
						Currency: "RUB",
					},
					VatCode:        1,
					PaymentMode:    "full_payment",
					PaymentSubject: "service",
				},
			},
		},
	}

	paymentHandler := yookassa.NewPaymentHandler(c.client).WithIdempotencyKey(idempotenceKey)
	result, err := c.breaker.Execute(func() (*yoopayment.Payment, error) {
		return paymentHandler.CreatePayment(payment)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create payment: %w", wrapBreakerErr(err))
	}

	c.logger.Info("Payment created successfully in YooKassa", "payment_id", result.ID, "status", result.Status)
	return result, nil
}

// GetPaymentStatus gets payment status from YooKassa
func (c *Client) GetPaymentStatus(ctx context.Context, paymentID string) (*yoopayment.Payment, error) {
	c.logger.Info("Getting payment status from YooKassa", "payment_id", paymentID)

	paymentHandler := yookassa.NewPaymentHandler(c.client)
	result, err := c.breaker.Execute(func() (*yoopayment.Payment, error) {
		return paymentHandler.FindPayment(paymentID)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get payment status: %w", wrapBreakerErr(err))
	}

	c.logger.Info("Payment status retrieved", "payment_id", paymentID, "status", result.Status)
	return result, nil
}

// CancelPayment cancels a pending payment in YooKassa
func (c *Client) CancelPayment(ctx context.Context, paymentID string) error {
	c.logger.Info("Cancelling payment in YooKassa", "payment_id", paymentID)

	idempotenceKey := fmt.Sprintf("cancel_%s_%d", paymentID, time.Now().Unix())

	paymentHandler := yookassa.NewPaymentHandler(c.client).WithIdempotencyKey(idempotenceKey)
	_, err := c.breaker.Execute(func() (*yoopayment.Payment, error) {
		return paymentHandler.CancelPayment(paymentID)
	})
	if err != nil {
		return fmt.Errorf("failed to cancel payment: %w", wrapBreakerErr(err))
	}

	c.logger.Info("Payment cancelled in YooKassa", "payment_id", paymentID)
	return nil
}
