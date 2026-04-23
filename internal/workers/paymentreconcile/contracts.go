package paymentreconcile

import (
	"context"

	"kurut-bot/internal/stories/payment"
)

type (
	// PaymentStorage lists payments by criteria (used here to find stale pending ones).
	PaymentStorage interface {
		ListPayments(ctx context.Context, criteria payment.ListCriteria) ([]*payment.Payment, error)
		ListOrphanedPayments(ctx context.Context) ([]*payment.Payment, error)
	}

	// PaymentService cancels a payment (idempotent + YooKassa race-safe).
	// CancelPayment:
	//   - Re-checks YooKassa status first
	//   - If YooKassa says "succeeded" already, DB is promoted to approved and
	//     ErrPaymentAlreadyApproved is returned — we treat that as "do not touch".
	//   - Otherwise it asks YooKassa to cancel the session and flips DB to cancelled.
	PaymentService interface {
		CancelPayment(ctx context.Context, paymentID int64) error
	}
)
