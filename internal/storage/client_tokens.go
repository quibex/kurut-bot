package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kurut-bot/internal/stories/webtokens"

	sq "github.com/Masterminds/squirrel"
)

const clientTokensTable = "client_tokens"

var clientTokenRowFields = fields(clientTokenRow{})

type clientTokenRow struct {
	ID                  int64     `db:"id"`
	Token               string    `db:"token"`
	WhatsApp            string    `db:"whatsapp"`
	CreatedByTelegramID int64     `db:"created_by_telegram_id"`
	CreatedAt           time.Time `db:"created_at"`
}

func (r clientTokenRow) ToModel() *webtokens.ClientToken {
	return &webtokens.ClientToken{
		ID:                  r.ID,
		Token:               r.Token,
		WhatsApp:            r.WhatsApp,
		CreatedByTelegramID: r.CreatedByTelegramID,
		CreatedAt:           r.CreatedAt,
	}
}

// GetClientTokenByToken finds client token by token string
func (s *storageImpl) GetClientTokenByToken(ctx context.Context, token string) (*webtokens.ClientToken, error) {
	q, args, err := s.stmpBuilder().
		Select(clientTokenRowFields).
		From(clientTokensTable).
		Where(sq.Eq{"token": token}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row clientTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

// GetClientTokenByWhatsApp finds client token by whatsapp
func (s *storageImpl) GetClientTokenByWhatsApp(ctx context.Context, whatsapp string) (*webtokens.ClientToken, error) {
	q, args, err := s.stmpBuilder().
		Select(clientTokenRowFields).
		From(clientTokensTable).
		Where(sq.Eq{"whatsapp": whatsapp}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row clientTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

// CreateClientToken creates a new client token
func (s *storageImpl) CreateClientToken(ctx context.Context, token webtokens.ClientToken) (*webtokens.ClientToken, error) {
	now := s.now()

	params := map[string]interface{}{
		"token":                  token.Token,
		"whatsapp":               token.WhatsApp,
		"created_by_telegram_id": token.CreatedByTelegramID,
		"created_at":             now,
	}

	q, args, err := s.stmpBuilder().
		Insert(clientTokensTable).
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

	return s.GetClientTokenByID(ctx, id)
}

// GetClientTokenByID finds client token by ID
func (s *storageImpl) GetClientTokenByID(ctx context.Context, id int64) (*webtokens.ClientToken, error) {
	q, args, err := s.stmpBuilder().
		Select(clientTokenRowFields).
		From(clientTokensTable).
		Where(sq.Eq{"id": id}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row clientTokenRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

// GetOrCreateClientToken returns existing token for whatsapp or creates new one
func (s *storageImpl) GetOrCreateClientToken(ctx context.Context, whatsapp string, createdByTelegramID int64) (*webtokens.ClientToken, error) {
	// Try to find existing
	existing, err := s.GetClientTokenByWhatsApp(ctx, whatsapp)
	if err != nil {
		return nil, fmt.Errorf("get by whatsapp: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Generate new token
	tokenStr, err := webtokens.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	// Create new
	newToken := webtokens.ClientToken{
		Token:               tokenStr,
		WhatsApp:            whatsapp,
		CreatedByTelegramID: createdByTelegramID,
	}

	return s.CreateClientToken(ctx, newToken)
}
