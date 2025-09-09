package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"kurut-bot/internal/stories/tariffs"
)

const tariffsTable = "tariffs"

var tariffRowFields = fields(tariffRow{})

type tariffRow struct {
	ID             int64     `db:"id"`
	Name           string    `db:"name"`
	DurationDays   int       `db:"duration_days"`
	Price          float64   `db:"price"`
	TrafficLimitGB *int      `db:"traffic_limit_gb"`
	IsActive     bool      `db:"is_active"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

func (t tariffRow) ToModel() *tariffs.Tariff {
	return &tariffs.Tariff{
		ID:             t.ID,
		Name:           t.Name,
		DurationDays:   t.DurationDays,
		Price:          t.Price,
		TrafficLimitGB: t.TrafficLimitGB,
		IsActive:     t.IsActive,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

func (s *storageImpl) CreateTariff(ctx context.Context, tariff tariffs.Tariff) (*tariffs.Tariff, error) {
	params := map[string]interface{}{
		"name":             tariff.Name,
		"duration_days":    tariff.DurationDays,
		"price":            tariff.Price,
		"traffic_limit_gb": tariff.TrafficLimitGB,
		"is_active":      tariff.IsActive,
		"created_at":       s.now(),
		"updated_at":       s.now(),
	}

	q, args, err := s.stmpBuilder().
		Insert(tariffsTable).
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

	return s.GetTariff(ctx, tariffs.GetCriteria{ID: &id})
}

func (s *storageImpl) GetTariff(ctx context.Context, criteria tariffs.GetCriteria) (*tariffs.Tariff, error) {
	query := s.stmpBuilder().
		Select(tariffRowFields).
		From(tariffsTable).
		Limit(1)

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	row := s.db.QueryRowContext(ctx, q, args...)

	var t tariffRow
	err = row.Scan(&t.ID, &t.Name, &t.DurationDays, &t.Price, &t.TrafficLimitGB, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("row.Scan: %w", err)
	}

	return t.ToModel(), nil
}

func (s *storageImpl) UpdateTariff(ctx context.Context, criteria tariffs.GetCriteria, params tariffs.UpdateParams) (*tariffs.Tariff, error) {
	query := s.stmpBuilder().
		Update(tariffsTable).
		Set("updated_at", s.now())

	// Добавляем условия для обновления
	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}

	// Добавляем параметры для обновления
	if params.Name != nil {
		query = query.Set("name", *params.Name)
	}
	if params.DurationDays != nil {
		query = query.Set("duration_days", *params.DurationDays)
	}
	if params.Price != nil {
		query = query.Set("price", *params.Price)
	}
	if params.TrafficLimitGB != nil {
		query = query.Set("traffic_limit_gb", *params.TrafficLimitGB)
	}
	if params.IsActive != nil {
		query = query.Set("is_active", *params.IsActive)
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	return s.GetTariff(ctx, criteria)
}

func (s *storageImpl) ListTariffs(ctx context.Context, criteria tariffs.ListCriteria) ([]*tariffs.Tariff, error) {
	query := s.stmpBuilder().
		Select(tariffRowFields).
		From(tariffsTable)

	if criteria.IsActive != nil {
		query = query.Where(sq.Eq{"is_active": *criteria.IsActive})
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

	var result []*tariffs.Tariff
	for rows.Next() {
		var t tariffRow
		err = rows.Scan(&t.ID, &t.Name, &t.DurationDays, &t.Price, &t.TrafficLimitGB, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, t.ToModel())
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}

func (s *storageImpl) DeleteTariff(ctx context.Context, criteria tariffs.DeleteCriteria) error {
	query := s.stmpBuilder().Delete(tariffsTable)

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
