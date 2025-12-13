package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kurut-bot/internal/stories/orders"

	sq "github.com/Masterminds/squirrel"
)

const pendingOrdersTable = "pending_orders"

var pendingOrderRowFields = fields(pendingOrderRow{})

type pendingOrderRow struct {
	ID                  int64     `db:"id"`
	PaymentID           int64     `db:"payment_id"`
	AdminUserID         int64     `db:"admin_user_id"`
	AssistantTelegramID int64     `db:"assistant_telegram_id"`
	ChatID              int64     `db:"chat_id"`
	MessageID           *int      `db:"message_id"`
	ClientWhatsApp      string    `db:"client_whatsapp"`
	TariffID            int64     `db:"tariff_id"`
	TariffName          string    `db:"tariff_name"`
	TotalAmount         float64   `db:"total_amount"`
	Status              string    `db:"status"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

func (r pendingOrderRow) ToModel() *orders.PendingOrder {
	return &orders.PendingOrder{
		ID:                  r.ID,
		PaymentID:           r.PaymentID,
		AdminUserID:         r.AdminUserID,
		AssistantTelegramID: r.AssistantTelegramID,
		ChatID:              r.ChatID,
		MessageID:           r.MessageID,
		ClientWhatsApp:      r.ClientWhatsApp,
		TariffID:            r.TariffID,
		TariffName:          r.TariffName,
		TotalAmount:         r.TotalAmount,
		Status:              orders.Status(r.Status),
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}

func (s *storageImpl) CreatePendingOrder(ctx context.Context, order orders.PendingOrder) (*orders.PendingOrder, error) {
	now := s.now()

	params := map[string]interface{}{
		"payment_id":            order.PaymentID,
		"admin_user_id":         order.AdminUserID,
		"assistant_telegram_id": order.AssistantTelegramID,
		"chat_id":               order.ChatID,
		"message_id":            order.MessageID,
		"client_whatsapp":       order.ClientWhatsApp,
		"tariff_id":             order.TariffID,
		"tariff_name":           order.TariffName,
		"total_amount":          order.TotalAmount,
		"status":                string(orders.StatusPending),
		"created_at":            now,
		"updated_at":            now,
	}

	q, args, err := s.stmpBuilder().
		Insert(pendingOrdersTable).
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

	return s.GetPendingOrderByID(ctx, id)
}

func (s *storageImpl) GetPendingOrderByID(ctx context.Context, id int64) (*orders.PendingOrder, error) {
	q, args, err := s.stmpBuilder().
		Select(pendingOrderRowFields).
		From(pendingOrdersTable).
		Where(sq.Eq{"id": id}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row pendingOrderRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

func (s *storageImpl) UpdatePendingOrderMessageID(ctx context.Context, id int64, messageID int) error {
	params := map[string]interface{}{
		"message_id": messageID,
		"updated_at": s.now(),
	}

	q, args, err := s.stmpBuilder().
		Update(pendingOrdersTable).
		SetMap(params).
		Where(sq.Eq{"id": id}).
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

func (s *storageImpl) UpdatePendingOrderPaymentID(ctx context.Context, id int64, paymentID int64) error {
	params := map[string]interface{}{
		"payment_id": paymentID,
		"updated_at": s.now(),
	}

	q, args, err := s.stmpBuilder().
		Update(pendingOrdersTable).
		SetMap(params).
		Where(sq.Eq{"id": id}).
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

func (s *storageImpl) UpdatePendingOrderStatus(ctx context.Context, id int64, status orders.Status) error {
	params := map[string]interface{}{
		"status":     string(status),
		"updated_at": s.now(),
	}

	q, args, err := s.stmpBuilder().
		Update(pendingOrdersTable).
		SetMap(params).
		Where(sq.Eq{"id": id}).
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

func (s *storageImpl) DeletePendingOrder(ctx context.Context, id int64) error {
	q, args, err := s.stmpBuilder().
		Delete(pendingOrdersTable).
		Where(sq.Eq{"id": id}).
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
