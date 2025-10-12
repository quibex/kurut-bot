package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"kurut-bot/internal/stories/payment"
)

const (
	paymentsTable             = "payments"
	paymentSubscriptionsTable = "payment_subscriptions"
)

var paymentRowFields = fields(paymentRow{})

type paymentRow struct {
	ID          int64      `db:"id"`
	UserID      int64      `db:"user_id"`
	Amount      float64    `db:"amount"`
	Status      string     `db:"status"`
	YooKassaID  *string    `db:"yookassa_id"`
	PaymentURL  *string    `db:"payment_url"`
	ProcessedAt *time.Time `db:"processed_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

func (p paymentRow) ToModel() *payment.Payment {
	return &payment.Payment{
		ID:          p.ID,
		UserID:      p.UserID,
		Amount:      p.Amount,
		Status:      payment.Status(p.Status),
		YooKassaID:  p.YooKassaID,
		PaymentURL:  p.PaymentURL,
		ProcessedAt: p.ProcessedAt,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func (s *storageImpl) CreatePayment(ctx context.Context, paymentEntity payment.Payment) (*payment.Payment, error) {
	params := map[string]interface{}{
		"user_id":      paymentEntity.UserID,
		"amount":       paymentEntity.Amount,
		"status":       string(paymentEntity.Status),
		"yookassa_id":  paymentEntity.YooKassaID,
		"payment_url":  paymentEntity.PaymentURL,
		"processed_at": paymentEntity.ProcessedAt,
		"created_at":   s.now(),
		"updated_at":   s.now(),
	}

	q, args, err := s.stmpBuilder().
		Insert(paymentsTable).
		SetMap(params).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	result, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("result.LastInsertId: %w", err)
	}

	return s.GetPayment(ctx, payment.GetCriteria{ID: &id})
}

func (s *storageImpl) GetPayment(ctx context.Context, criteria payment.GetCriteria) (*payment.Payment, error) {
	query := s.stmpBuilder().
		Select(paymentRowFields).
		From(paymentsTable).
		Limit(1)

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}
	if criteria.YooKassaID != nil {
		query = query.Where(sq.Eq{"yookassa_id": *criteria.YooKassaID})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, q, args...)

	var p paymentRow
	err = row.Scan(&p.ID, &p.UserID, &p.Amount, &p.Status, &p.YooKassaID,
		&p.PaymentURL, &p.ProcessedAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("row.Scan: %w", err)
	}

	return p.ToModel(), nil
}

func (s *storageImpl) UpdatePayment(ctx context.Context, criteria payment.GetCriteria, params payment.UpdateParams) (*payment.Payment, error) {
	query := s.stmpBuilder().
		Update(paymentsTable).
		Set("updated_at", s.now())

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}
	if criteria.YooKassaID != nil {
		query = query.Where(sq.Eq{"yookassa_id": *criteria.YooKassaID})
	}

	if params.Status != nil {
		query = query.Set("status", string(*params.Status))
	}
	if params.YooKassaID != nil {
		query = query.Set("yookassa_id", *params.YooKassaID)
	}
	if params.PaymentURL != nil {
		query = query.Set("payment_url", *params.PaymentURL)
	}
	if params.ProcessedAt != nil {
		query = query.Set("processed_at", *params.ProcessedAt)
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	return s.GetPayment(ctx, criteria)
}

func (s *storageImpl) ListPayments(ctx context.Context, criteria payment.ListCriteria) ([]*payment.Payment, error) {
	query := s.stmpBuilder().
		Select(paymentRowFields).
		From(paymentsTable)

	if criteria.UserID != nil {
		query = query.Where(sq.Eq{"user_id": *criteria.UserID})
	}
	if criteria.Status != nil {
		query = query.Where(sq.Eq{"status": string(*criteria.Status)})
	}

	if criteria.Limit > 0 {
		query = query.Limit(uint64(criteria.Limit))
	}
	if criteria.Offset > 0 {
		query = query.Offset(uint64(criteria.Offset))
	}

	query = query.OrderBy("created_at DESC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.QueryContext: %w", err)
	}
	defer rows.Close()

	var result []*payment.Payment
	for rows.Next() {
		var p paymentRow
		err = rows.Scan(&p.ID, &p.UserID, &p.Amount, &p.Status, &p.YooKassaID,
			&p.PaymentURL, &p.ProcessedAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, p.ToModel())
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}

func (s *storageImpl) DeletePayment(ctx context.Context, criteria payment.DeleteCriteria) error {
	query := s.stmpBuilder().Delete(paymentsTable)

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("db.ExecContext: %w", err)
	}

	return nil
}

// Payment-Subscription связи
func (s *storageImpl) CreatePaymentSubscription(ctx context.Context, req payment.CreatePaymentSubscriptionRequest) error {
	params := map[string]interface{}{
		"payment_id":      req.PaymentID,
		"subscription_id": req.SubscriptionID,
	}

	q, args, err := s.stmpBuilder().
		Insert(paymentSubscriptionsTable).
		SetMap(params).
		ToSql()
	if err != nil {
		return fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("db.ExecContext: %w", err)
	}

	return nil
}

func (s *storageImpl) GetPaymentSubscriptions(ctx context.Context, paymentID int64) ([]int64, error) {
	q, args, err := s.stmpBuilder().
		Select("subscription_id").
		From(paymentSubscriptionsTable).
		Where(sq.Eq{"payment_id": paymentID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.QueryContext: %w", err)
	}
	defer rows.Close()

	var result []int64
	for rows.Next() {
		var subscriptionID int64
		err = rows.Scan(&subscriptionID)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, subscriptionID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}

func (s *storageImpl) DeletePaymentSubscriptions(ctx context.Context, paymentID int64) error {
	q, args, err := s.stmpBuilder().
		Delete(paymentSubscriptionsTable).
		Where(sq.Eq{"payment_id": paymentID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("db.ExecContext: %w", err)
	}

	return nil
}

// LinkPaymentToSubscriptions creates links between payment and multiple subscriptions
func (s *storageImpl) LinkPaymentToSubscriptions(ctx context.Context, paymentID int64, subscriptionIDs []int64) error {
	for _, subscriptionID := range subscriptionIDs {
		req := payment.CreatePaymentSubscriptionRequest{
			PaymentID:      paymentID,
			SubscriptionID: subscriptionID,
		}
		if err := s.CreatePaymentSubscription(ctx, req); err != nil {
			return fmt.Errorf("failed to link payment %d to subscription %d: %w", paymentID, subscriptionID, err)
		}
	}
	return nil
}

// ListOrphanedPayments returns approved payments that have no linked subscriptions
func (s *storageImpl) ListOrphanedPayments(ctx context.Context) ([]*payment.Payment, error) {
	query := `
		SELECT ` + paymentRowFields + `
		FROM ` + paymentsTable + ` p
		LEFT JOIN ` + paymentSubscriptionsTable + ` ps ON p.id = ps.payment_id
		WHERE p.status = ?
		AND ps.payment_id IS NULL
		ORDER BY p.created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, string(payment.StatusApproved))
	if err != nil {
		return nil, fmt.Errorf("db.QueryContext: %w", err)
	}
	defer rows.Close()

	var result []*payment.Payment
	for rows.Next() {
		var p paymentRow
		err = rows.Scan(&p.ID, &p.UserID, &p.Amount, &p.Status, &p.YooKassaID,
			&p.PaymentURL, &p.ProcessedAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, p.ToModel())
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}
