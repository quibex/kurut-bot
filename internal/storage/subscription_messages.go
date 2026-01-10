package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kurut-bot/internal/stories/submessages"

	sq "github.com/Masterminds/squirrel"
)

const subscriptionMessagesTable = "subscription_messages"

var subscriptionMessageRowFields = fields(subscriptionMessageRow{})

type subscriptionMessageRow struct {
	ID               int64     `db:"id"`
	SubscriptionID   int64     `db:"subscription_id"`
	ChatID           int64     `db:"chat_id"`
	MessageID        int       `db:"message_id"`
	Type             string    `db:"type"`
	IsActive         bool      `db:"is_active"`
	SelectedTariffID *int64    `db:"selected_tariff_id"`
	PaymentID        *int64    `db:"payment_id"`
	CreatedAt        time.Time `db:"created_at"`
}

func (r subscriptionMessageRow) ToModel() *submessages.SubscriptionMessage {
	return &submessages.SubscriptionMessage{
		ID:               r.ID,
		SubscriptionID:   r.SubscriptionID,
		ChatID:           r.ChatID,
		MessageID:        r.MessageID,
		Type:             submessages.Type(r.Type),
		IsActive:         r.IsActive,
		SelectedTariffID: r.SelectedTariffID,
		PaymentID:        r.PaymentID,
		CreatedAt:        r.CreatedAt,
	}
}

// CreateSubscriptionMessage creates a new subscription message record
func (s *storageImpl) CreateSubscriptionMessage(ctx context.Context, msg submessages.SubscriptionMessage) (*submessages.SubscriptionMessage, error) {
	params := map[string]interface{}{
		"subscription_id":    msg.SubscriptionID,
		"chat_id":            msg.ChatID,
		"message_id":         msg.MessageID,
		"type":               string(msg.Type),
		"is_active":          true,
		"selected_tariff_id": msg.SelectedTariffID,
		"created_at":         s.now(),
	}

	q, args, err := s.stmpBuilder().
		Insert(subscriptionMessagesTable).
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

	return s.GetSubscriptionMessageByID(ctx, id)
}

// GetSubscriptionMessageByID returns a subscription message by ID
func (s *storageImpl) GetSubscriptionMessageByID(ctx context.Context, id int64) (*submessages.SubscriptionMessage, error) {
	query := s.stmpBuilder().
		Select(subscriptionMessageRowFields).
		From(subscriptionMessagesTable).
		Where(sq.Eq{"id": id}).
		Limit(1)

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row subscriptionMessageRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

// GetSubscriptionMessageByChatAndMessageID returns a subscription message by chat_id and message_id
func (s *storageImpl) GetSubscriptionMessageByChatAndMessageID(ctx context.Context, chatID int64, messageID int) (*submessages.SubscriptionMessage, error) {
	query := s.stmpBuilder().
		Select(subscriptionMessageRowFields).
		From(subscriptionMessagesTable).
		Where(sq.Eq{"chat_id": chatID}).
		Where(sq.Eq{"message_id": messageID}).
		Limit(1)

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row subscriptionMessageRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

// ListActiveSubscriptionMessages returns all active messages for a subscription
func (s *storageImpl) ListActiveSubscriptionMessages(ctx context.Context, subscriptionID int64) ([]*submessages.SubscriptionMessage, error) {
	query := s.stmpBuilder().
		Select(subscriptionMessageRowFields).
		From(subscriptionMessagesTable).
		Where(sq.Eq{"subscription_id": subscriptionID}).
		Where(sq.Eq{"is_active": true}).
		OrderBy("created_at DESC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var rows []subscriptionMessageRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	var messages []*submessages.SubscriptionMessage
	for _, row := range rows {
		messages = append(messages, row.ToModel())
	}

	return messages, nil
}

// DeactivateSubscriptionMessage marks a message as inactive
func (s *storageImpl) DeactivateSubscriptionMessage(ctx context.Context, id int64) error {
	q, args, err := s.stmpBuilder().
		Update(subscriptionMessagesTable).
		Set("is_active", false).
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

// DeactivateAllSubscriptionMessages marks all messages for a subscription as inactive
func (s *storageImpl) DeactivateAllSubscriptionMessages(ctx context.Context, subscriptionID int64) error {
	q, args, err := s.stmpBuilder().
		Update(subscriptionMessagesTable).
		Set("is_active", false).
		Where(sq.Eq{"subscription_id": subscriptionID}).
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

// UpdateSelectedTariff updates the selected tariff for a subscription message
func (s *storageImpl) UpdateSelectedTariff(ctx context.Context, id int64, tariffID *int64) error {
	q, args, err := s.stmpBuilder().
		Update(subscriptionMessagesTable).
		Set("selected_tariff_id", tariffID).
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

// UpdatePaymentID updates the payment ID for a subscription message
func (s *storageImpl) UpdatePaymentID(ctx context.Context, id int64, paymentID *int64) error {
	q, args, err := s.stmpBuilder().
		Update(subscriptionMessagesTable).
		Set("payment_id", paymentID).
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
