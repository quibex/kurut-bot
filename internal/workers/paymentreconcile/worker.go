// Package paymentreconcile runs once per day (@every 24h) to close out payments
// that have been stuck in "pending" for too long (default: 14 days).
//
// Why 14 days:
//   - YooKassa card-payment checkout sessions typically finalize within an hour
//     (user pays → status=succeeded, or user abandons → YooKassa auto-cancels
//     and status=canceled). The paymentautocheck worker catches both within 30s.
//   - 14 days is chosen as a generous safety margin for edge cases where YooKassa
//     itself keeps the session in pending unusually long. It is large enough that
//     a user who wants to pay has had ample time, but small enough that pending
//     entries don't accumulate forever and poison success-rate analytics.
//
// Race-condition safety:
//   - Cancellation goes through PaymentService.CancelPayment, which first queries
//     YooKassa for the current status. If YooKassa reports the payment as
//     actually succeeded, the local DB is flipped to approved and we get back
//     payment.ErrPaymentAlreadyApproved — we do NOT overwrite the status. In
//     that case the payment will be picked up by the normal paymentautocheck
//     flow (subscription creation through its pending_order).
//   - If YooKassa is still pending, we issue an explicit Cancel API call
//     (atomically blocking further capture) and then flip local DB to cancelled.
package paymentreconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"kurut-bot/internal/stories/payment"

	"github.com/robfig/cron/v3"
)

const (
	// StalePendingAgeDays is how old a pending payment must be before we try
	// to close it. YooKassa sessions finalize within hours in normal operation;
	// this is a deliberately conservative margin.
	StalePendingAgeDays = 14

	// interval is how often we scan. Once per day is enough — this is cleanup,
	// not real-time flow.
	interval = "@every 24h"
)

// Worker cleans up stale pending payments by reconciling against YooKassa.
type Worker struct {
	paymentStorage PaymentStorage
	paymentService PaymentService
	logger         *slog.Logger
	cron           *cron.Cron
}

// NewWorker constructs a reconciliation worker.
func NewWorker(
	paymentStorage PaymentStorage,
	paymentService PaymentService,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		paymentStorage: paymentStorage,
		paymentService: paymentService,
		logger:         logger,
		cron:           cron.New(),
	}
}

// Name returns the worker identifier for manager logging.
func (w *Worker) Name() string {
	return "payment-reconcile"
}

// Start schedules the reconciliation on a daily interval.
func (w *Worker) Start() error {
	_, err := w.cron.AddFunc(interval, func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in payment-reconcile worker", "panic", r)
			}
		}()
		ctx := context.Background()
		if err := w.run(ctx); err != nil {
			w.logger.Error("Payment-reconcile run failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("schedule payment-reconcile: %w", err)
	}

	w.cron.Start()
	w.logger.Info("Payment-reconcile worker started",
		"interval", interval,
		"stale_age_days", StalePendingAgeDays)
	return nil
}

// Stop halts the cron schedule.
func (w *Worker) Stop() {
	w.logger.Info("Stopping payment-reconcile worker")
	w.cron.Stop()
}

// run performs a single reconciliation sweep.
func (w *Worker) run(ctx context.Context) error {
	stats := reconcileStats{}

	if err := w.reconcileStalePending(ctx, &stats); err != nil {
		w.logger.Error("Reconcile stale pending failed", "error", err)
	}

	if err := w.alertOrphanedApproved(ctx, &stats); err != nil {
		w.logger.Error("Orphaned-payments scan failed", "error", err)
	}

	w.logger.Info("Payment-reconcile run complete",
		"scanned_stale_pending", stats.scanned,
		"cancelled", stats.cancelled,
		"promoted_to_approved", stats.promotedToApproved,
		"errored", stats.errored,
		"orphans_found", stats.orphansFound,
	)

	return nil
}

type reconcileStats struct {
	scanned            int
	cancelled          int
	promotedToApproved int
	errored            int
	orphansFound       int
}

// reconcileStalePending closes out pending payments older than StalePendingAgeDays.
// For each payment we delegate to PaymentService.CancelPayment, which performs
// a YooKassa round-trip and is the authoritative, race-safe path.
func (w *Worker) reconcileStalePending(ctx context.Context, stats *reconcileStats) error {
	cutoff := time.Now().Add(-StalePendingAgeDays * 24 * time.Hour)
	pendingStatus := payment.StatusPending

	stale, err := w.paymentStorage.ListPayments(ctx, payment.ListCriteria{
		Status:        &pendingStatus,
		CreatedBefore: &cutoff,
	})
	if err != nil {
		return fmt.Errorf("list stale pending payments: %w", err)
	}

	stats.scanned = len(stale)
	if len(stale) == 0 {
		return nil
	}

	w.logger.Info("Reconciling stale pending payments",
		"count", len(stale),
		"cutoff", cutoff.Format(time.RFC3339))

	for _, p := range stale {
		err := w.paymentService.CancelPayment(ctx, p.ID)
		switch {
		case err == nil:
			stats.cancelled++
			w.logger.Info("Stale pending payment cancelled via YooKassa sync",
				"payment_id", p.ID,
				"age_days", int(time.Since(p.CreatedAt).Hours()/24),
				"has_yookassa_id", p.YooKassaID != nil)

		case errors.Is(err, payment.ErrPaymentAlreadyApproved):
			// The payment was actually paid in YooKassa and the local DB has
			// just been promoted to approved by CancelPayment. Do NOT cancel.
			// paymentautocheck will pick this up on its next tick if there is a
			// pending_order. If there is not → this becomes an orphan, flagged
			// by alertOrphanedApproved below.
			stats.promotedToApproved++
			w.logger.Warn("Stale 'pending' payment was actually approved in YooKassa — promoted locally",
				"payment_id", p.ID,
				"age_days", int(time.Since(p.CreatedAt).Hours()/24),
				"note", "paymentautocheck will activate the subscription on next tick if pending_order exists; otherwise orphan alert will fire")

		default:
			stats.errored++
			w.logger.Error("Failed to reconcile stale pending payment",
				"payment_id", p.ID,
				"error", err)
		}
	}

	return nil
}

// alertOrphanedApproved logs an alertable warning for each approved payment
// with no linked subscription. This is a data-integrity invariant: a user paid
// but received nothing. Requires human intervention. The monitoring exporter
// already exposes kurut_bot_orphaned_payments as a gauge.
func (w *Worker) alertOrphanedApproved(ctx context.Context, stats *reconcileStats) error {
	orphans, err := w.paymentStorage.ListOrphanedPayments(ctx)
	if err != nil {
		return fmt.Errorf("list orphaned payments: %w", err)
	}

	stats.orphansFound = len(orphans)
	for _, p := range orphans {
		w.logger.Warn("Orphaned approved payment — money taken, no subscription linked",
			"payment_id", p.ID,
			"user_id", p.UserID,
			"amount", p.Amount,
			"yookassa_id", p.YooKassaID,
			"age_hours", int(time.Since(p.CreatedAt).Hours()))
	}

	return nil
}
