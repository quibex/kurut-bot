package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"kurut-bot/internal/stories/assistants"
)

const assistantsTable = "assistants"

var assistantRowFields = fields(assistantRow{})

type assistantRow struct {
	ID         int64     `db:"id"`
	TelegramID int64     `db:"telegram_id"`
	Label      *string   `db:"label"`
	AddedBy    *int64    `db:"added_by"`
	CreatedAt  time.Time `db:"created_at"`
}

func (a assistantRow) ToModel() *assistants.Assistant {
	m := &assistants.Assistant{
		ID:         a.ID,
		TelegramID: a.TelegramID,
		CreatedAt:  a.CreatedAt,
	}
	if a.Label != nil {
		m.Label = *a.Label
	}
	if a.AddedBy != nil {
		m.AddedBy = *a.AddedBy
	}
	return m
}

func (s *storageImpl) CreateAssistant(ctx context.Context, assistant assistants.Assistant) (*assistants.Assistant, error) {
	var label *string
	if assistant.Label != "" {
		label = &assistant.Label
	}
	var addedBy *int64
	if assistant.AddedBy != 0 {
		addedBy = &assistant.AddedBy
	}

	params := map[string]interface{}{
		"telegram_id": assistant.TelegramID,
		"label":       label,
		"added_by":    addedBy,
		"created_at":  s.now(),
	}

	q, args, err := s.stmpBuilder().
		Insert(assistantsTable).
		SetMap(params).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	tgID := assistant.TelegramID
	return s.GetAssistantByTelegramID(ctx, tgID)
}

func (s *storageImpl) GetAssistantByTelegramID(ctx context.Context, telegramID int64) (*assistants.Assistant, error) {
	q, args, err := s.stmpBuilder().
		Select(assistantRowFields).
		From(assistantsTable).
		Where(sq.Eq{"telegram_id": telegramID}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var row assistantRow
	err = s.db.GetContext(ctx, &row, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return row.ToModel(), nil
}

func (s *storageImpl) ListAssistants(ctx context.Context) ([]*assistants.Assistant, error) {
	q, args, err := s.stmpBuilder().
		Select(assistantRowFields).
		From(assistantsTable).
		OrderBy("created_at DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var rows []assistantRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	result := make([]*assistants.Assistant, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ToModel())
	}

	return result, nil
}

// ListAssistantTelegramIDs returns just the Telegram ids — used to warm the
// AdminChecker roster cache at startup.
func (s *storageImpl) ListAssistantTelegramIDs(ctx context.Context) ([]int64, error) {
	q, args, err := s.stmpBuilder().
		Select("telegram_id").
		From(assistantsTable).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var ids []int64
	err = s.db.SelectContext(ctx, &ids, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	return ids, nil
}

func (s *storageImpl) DeleteAssistant(ctx context.Context, criteria assistants.DeleteCriteria) error {
	query := s.stmpBuilder().Delete(assistantsTable)

	if criteria.TelegramID != nil {
		query = query.Where(sq.Eq{"telegram_id": *criteria.TelegramID})
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
