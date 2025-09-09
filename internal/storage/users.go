package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"kurut-bot/internal/stories/users"
)

const usersTable = "users"

var userRowFields = fields(userRow{})

type userRow struct {
	ID         int64     `db:"id"`
	TelegramID int64     `db:"telegram_id"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

func (u userRow) ToModel() *users.User {
	return &users.User{
		ID:         u.ID,
		TelegramID: u.TelegramID,
		CreatedAt:  u.CreatedAt,
		UpdatedAt:  u.UpdatedAt,
	}
}

func (s *storageImpl) CreateUser(ctx context.Context, user users.User) (*users.User, error) {
	params := map[string]interface{}{
		"telegram_id": user.TelegramID,
		"created_at":  s.now(),
		"updated_at":  s.now(),
	}

	q, args, err := s.stmpBuilder().
		Insert(usersTable).
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

	return s.GetUser(ctx, users.GetCriteria{ID: &id})
}

func (s *storageImpl) GetUser(ctx context.Context, criteria users.GetCriteria) (*users.User, error) {
	query := s.stmpBuilder().
		Select(userRowFields).
		From(usersTable).
		Limit(1)

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}
	if criteria.TelegramID != nil {
		query = query.Where(sq.Eq{"telegram_id": *criteria.TelegramID})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, q, args...)

	var u userRow
	err = row.Scan(&u.ID, &u.TelegramID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("row.Scan: %w", err)
	}

	return u.ToModel(), nil
}

// func (s *storageImpl) UpdateUser(ctx context.Context, criteria users.GetCriteria, params users.UpdateParams) (*users.User, error) {
// 	query := s.stmpBuilder().
// 		Update(usersTable).
// 		Set("updated_at", s.now())

// 	// Добавляем условия для обновления
// 	if criteria.ID != nil {
// 		query = query.Where(sq.Eq{"id": *criteria.ID})
// 	}
// 	if criteria.TelegramID != nil {
// 		query = query.Where(sq.Eq{"telegram_id": *criteria.TelegramID})
// 	}

// 	// Добавляем параметры для обновления
// 	if params.Username != nil {
// 		query = query.Set("username", *params.Username)
// 	}

// 	q, args, err := query.ToSql()
// 	if err != nil {
// 		return nil, fmt.Errorf("build sql query: %w", err)
// 	}

// 	_, err = s.db.ExecContext(ctx, q, args...)
// 	if err != nil {
// 		return nil, fmt.Errorf("db.ExecContext: %w", err)
// 	}

// 	return s.GetUser(ctx, criteria)
// }

func (s *storageImpl) ListUsers(ctx context.Context, criteria users.ListCriteria) ([]*users.User, error) {
	query := s.stmpBuilder().
		Select(userRowFields).
		From(usersTable)

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

	var result []*users.User
	for rows.Next() {
		var u userRow
		err = rows.Scan(&u.ID, &u.TelegramID, &u.CreatedAt, &u.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, u.ToModel())
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}

func (s *storageImpl) DeleteUser(ctx context.Context, criteria users.DeleteCriteria) error {
	query := s.stmpBuilder().Delete(usersTable)

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}
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
