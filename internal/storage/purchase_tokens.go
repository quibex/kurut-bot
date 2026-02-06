package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kurut-bot/internal/stories/webtokens"

	sq "github.com/Masterminds/squirrel"
)

const purchaseTokensTable = "purchase_tokens"

var purchaseTokenRowFields = fields(purchaseTokenRow{})

type purchaseTokenRow struct {
	ID                  int64     `db:"id"`
	Token               string    `db:"token"`
	ClientWhatsApp      string    `db:"client_whatsapp"`
	CreatedByTelegramID int64     `db:"created_by_telegram_id"`
	TariffID            *int64    `db:"tariff_id"`
	ReferrerWhatsApp    *string   `db:"referrer_whatsapp"`
	PaymentID           *int64    `db:"payment_id"`
	Status              string    `db:"status"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

func (r purchaseTokenRow) ToModel() *webtokens.PurchaseToken {
	return &webtokens.PurchaseToken{
		ID:                  r.ID,
		Token:               r.Token,
		ClientWhatsApp:      r.ClientWhatsApp,
		CreatedByTelegramID: r.CreatedByTelegramID,
		TariffID:            r.TariffID,
		ReferrerWhatsApp:    r.ReferrerWhatsApp,
		PaymentID:           r.PaymentID,
		Status:              webtokens.PurchaseTokenStatus(r.Status),
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}

func (s *storageImpl) CreatePurchaseToken(ctx context.Context, token webtokens.PurchaseToken) (*webtokens.PurchaseToken, error) {
	now := s.now()

	params := map[string]interface{}{
		"token":                  token.Token,
		"client_whatsapp":        token.ClientWhatsApp,
		"created_by_telegram_id": token.CreatedByTelegramID,
		"tariff_id":              token.TariffID,
		"referrer_whatsapp":      token.ReferrerWhatsApp,
		"payment_id":             token.PaymentID,
		"status":                 string(webtokens.PurchaseStatusPending),
		"created_at":             now,
		"updated_at":             now,
	}

	q, args, err := s.stmpBuilder().
		Insert(purchaseTokensTable).
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

	return s.GetPurchaseTokenByID(ctx, id)
}

func (s *storageImpl) GetPurchaseTokenByID(ctx context.Context, id int64) (*webtokens.PurchaseToken, error) {
	q, args, err := s.stmpBuilder().
		Select(purchaseTokenRowFields).
		From(purchaseTokensTable).
		Where(sq.Eq{"id": id}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row purchaseTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

func (s *storageImpl) GetPurchaseTokenByToken(ctx context.Context, token string) (*webtokens.PurchaseToken, error) {
	q, args, err := s.stmpBuilder().
		Select(purchaseTokenRowFields).
		From(purchaseTokensTable).
		Where(sq.Eq{"token": token}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row purchaseTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

func (s *storageImpl) UpdatePurchaseToken(ctx context.Context, id int64, tariffID int64, referrerWhatsApp *string, paymentID int64) error {
	params := map[string]interface{}{
		"tariff_id":         tariffID,
		"referrer_whatsapp": referrerWhatsApp,
		"payment_id":        paymentID,
		"status":            string(webtokens.PurchaseStatusPaid),
		"updated_at":        s.now(),
	}

	q, args, err := s.stmpBuilder().
		Update(purchaseTokensTable).
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

func (s *storageImpl) UpdatePurchaseTokenStatus(ctx context.Context, id int64, status webtokens.PurchaseTokenStatus) error {
	params := map[string]interface{}{
		"status":     string(status),
		"updated_at": s.now(),
	}

	q, args, err := s.stmpBuilder().
		Update(purchaseTokensTable).
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

// ListPaidPurchaseTokens returns all purchase tokens with status 'paid' and payment_id
func (s *storageImpl) ListPaidPurchaseTokens(ctx context.Context) ([]*webtokens.PurchaseToken, error) {
	q, args, err := s.stmpBuilder().
		Select(purchaseTokenRowFields).
		From(purchaseTokensTable).
		Where(sq.Eq{"status": string(webtokens.PurchaseStatusPaid)}).
		Where(sq.NotEq{"payment_id": nil}).
		OrderBy("created_at ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var rows []purchaseTokenRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	var result []*webtokens.PurchaseToken
	for _, row := range rows {
		result = append(result, row.ToModel())
	}

	return result, nil
}
