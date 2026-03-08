package expiration

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/stories/subs"

	"github.com/robfig/cron/v3"
)

// Worker handles marking expired subscriptions
type Worker struct {
	storage Storage
	logger  *slog.Logger
	cron    *cron.Cron
}

// NewWorker creates a new expiration worker
func NewWorker(
	storage Storage,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		storage: storage,
		logger:  logger,
		cron:    cron.New(),
	}
}

// Name returns the worker name
func (w *Worker) Name() string {
	return "expiration"
}

// Start starts the expiration worker
func (w *Worker) Start() error {
	// Runs daily at 03:00 Moscow time
	_, err := w.cron.AddFunc("CRON_TZ=Europe/Moscow 0 3 * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("Panic in expiration worker", "panic", r)
			}
		}()
		ctx := context.Background()
		w.logger.Info("Running expiration worker")
		if err := w.run(ctx); err != nil {
			w.logger.Error("Expiration worker failed", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to schedule expiration worker: %w", err)
	}

	w.cron.Start()
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping expiration worker")
	w.cron.Stop()
}

// RunNow runs the worker immediately (for manual testing)
func (w *Worker) RunNow(ctx context.Context) error {
	w.logger.Info("Manual run of expiration worker")
	return w.run(ctx)
}

// run executes the expiration logic
func (w *Worker) run(ctx context.Context) error {
	w.logger.Info("Starting expiration worker execution")

	if err := w.markExpiredSubscriptions(ctx); err != nil {
		w.logger.Error("Failed to mark expired subscriptions", "error", err)
	}

	w.logger.Info("Expiration worker execution completed")
	return nil
}

// markExpiredSubscriptions marks expired subscriptions as expired in DB
func (w *Worker) markExpiredSubscriptions(ctx context.Context) error {
	subscriptions, err := w.storage.ListExpiredSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("list expired subscriptions: %w", err)
	}

	w.logger.Info("Marking expired subscriptions", "count", len(subscriptions))

	expiredStatus := subs.StatusExpired
	for _, sub := range subscriptions {
		criteria := subs.GetCriteria{IDs: []int64{sub.ID}}
		params := subs.UpdateParams{Status: &expiredStatus}

		_, err := w.storage.UpdateSubscription(ctx, criteria, params)
		if err != nil {
			w.logger.Error("Failed to expire subscription",
				"subscription_id", sub.ID,
				"error", err)
			continue
		}

		w.logger.Info("Subscription expired",
			"subscription_id", sub.ID,
			"user_id", sub.UserID)
	}

	return nil
}
