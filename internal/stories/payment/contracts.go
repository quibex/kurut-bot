package payment

import (
	"context"

	yoopayment "github.com/rvinnie/yookassa-sdk-go/yookassa/payment"
)

type (
	// Storage provides database operations for payments
	Storage interface {
		CreatePayment(ctx context.Context, payment Payment) (*Payment, error)
		GetPayment(ctx context.Context, criteria GetCriteria) (*Payment, error)
		UpdatePayment(ctx context.Context, criteria GetCriteria, params UpdateParams) (*Payment, error)
		ListPayments(ctx context.Context, criteria ListCriteria) ([]*Payment, error)
		DeletePayment(ctx context.Context, criteria DeleteCriteria) error
		LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error
		ListOrphanedPayments(ctx context.Context) ([]*Payment, error)
	}

	// YooKassaClient provides YooKassa API operations
	YooKassaClient interface {
		CreatePayment(ctx context.Context, amount float64, description string, metadata map[string]string) (*yoopayment.Payment, error)
		GetPaymentStatus(ctx context.Context, paymentID string) (*yoopayment.Payment, error)
	}
)
