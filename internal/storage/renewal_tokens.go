package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kurut-bot/internal/stories/webtokens"

	sq "github.com/Masterminds/squirrel"
)

const renewalTokensTable = "renewal_tokens"

var renewalTokenRowFields = fields(renewalTokenRow{})

type renewalTokenRow struct {
	ID             int64     `db:"id"`
	Token          string    `db:"token"`
	SubscriptionID int64     `db:"subscription_id"`
	CreatedAt      time.Time `db:"created_at"`
}

func (r renewalTokenRow) ToModel() *webtokens.RenewalToken {
	return &webtokens.RenewalToken{
		ID:             r.ID,
		Token:          r.Token,
		SubscriptionID: r.SubscriptionID,
		CreatedAt:      r.CreatedAt,
	}
}

func (s *storageImpl) CreateRenewalToken(ctx context.Context, token webtokens.RenewalToken) (*webtokens.RenewalToken, error) {
	now := s.now()

	params := map[string]interface{}{
		"token":           token.Token,
		"subscription_id": token.SubscriptionID,
		"created_at":      now,
	}

	q, args, err := s.stmpBuilder().
		Insert(renewalTokensTable).
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

	return s.GetRenewalTokenByID(ctx, id)
}

func (s *storageImpl) GetRenewalTokenByID(ctx context.Context, id int64) (*webtokens.RenewalToken, error) {
	q, args, err := s.stmpBuilder().
		Select(renewalTokenRowFields).
		From(renewalTokensTable).
		Where(sq.Eq{"id": id}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row renewalTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

func (s *storageImpl) GetRenewalTokenByToken(ctx context.Context, token string) (*webtokens.RenewalToken, error) {
	q, args, err := s.stmpBuilder().
		Select(renewalTokenRowFields).
		From(renewalTokensTable).
		Where(sq.Eq{"token": token}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row renewalTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

func (s *storageImpl) GetRenewalTokenBySubscriptionID(ctx context.Context, subscriptionID int64) (*webtokens.RenewalToken, error) {
	q, args, err := s.stmpBuilder().
		Select(renewalTokenRowFields).
		From(renewalTokensTable).
		Where(sq.Eq{"subscription_id": subscriptionID}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row renewalTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

// GetOrCreateRenewalToken returns existing token for subscription or creates a new one
func (s *storageImpl) GetOrCreateRenewalToken(ctx context.Context, subscriptionID int64) (*webtokens.RenewalToken, error) {
	existing, err := s.GetRenewalTokenBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	tokenStr, err := webtokens.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return s.CreateRenewalToken(ctx, webtokens.RenewalToken{
		Token:          tokenStr,
		SubscriptionID: subscriptionID,
	})
}
