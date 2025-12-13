package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"kurut-bot/internal/stories/servers"
)

const serversTable = "servers"

var serverRowFields = fields(serverRow{})

type serverRow struct {
	ID           int64     `db:"id"`
	Name         string    `db:"name"`
	UIURL        string    `db:"ui_url"`
	UIPassword   string    `db:"ui_password"`
	CurrentUsers int       `db:"current_users"`
	MaxUsers     int       `db:"max_users"`
	Archived     bool      `db:"archived"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

func (s serverRow) ToModel() *servers.Server {
	return &servers.Server{
		ID:           s.ID,
		Name:         s.Name,
		UIURL:        s.UIURL,
		UIPassword:   s.UIPassword,
		CurrentUsers: s.CurrentUsers,
		MaxUsers:     s.MaxUsers,
		Archived:     s.Archived,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

func (s *storageImpl) CreateServer(ctx context.Context, server servers.Server) (*servers.Server, error) {
	params := map[string]interface{}{
		"name":          server.Name,
		"ui_url":        server.UIURL,
		"ui_password":   server.UIPassword,
		"current_users": server.CurrentUsers,
		"max_users":     server.MaxUsers,
		"archived":      server.Archived,
		"created_at":    s.now(),
		"updated_at":    s.now(),
	}

	q, args, err := s.stmpBuilder().
		Insert(serversTable).
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

	return s.GetServer(ctx, servers.GetCriteria{ID: &id})
}

func (s *storageImpl) GetServer(ctx context.Context, criteria servers.GetCriteria) (*servers.Server, error) {
	query := s.stmpBuilder().
		Select(serverRowFields).
		From(serversTable).
		Limit(1)

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}
	if criteria.Archived != nil {
		query = query.Where(sq.Eq{"archived": *criteria.Archived})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var srv serverRow
	err = s.db.GetContext(ctx, &srv, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return srv.ToModel(), nil
}

func (s *storageImpl) ListServers(ctx context.Context, criteria servers.ListCriteria) ([]*servers.Server, error) {
	query := s.stmpBuilder().
		Select(serverRowFields).
		From(serversTable)

	if criteria.Archived != nil {
		query = query.Where(sq.Eq{"archived": *criteria.Archived})
	}

	if criteria.Limit > 0 {
		query = query.Limit(uint64(criteria.Limit))
	}
	if criteria.Offset > 0 {
		query = query.Offset(uint64(criteria.Offset))
	}

	query = query.OrderBy("created_at ASC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var rows []serverRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	var result []*servers.Server
	for _, row := range rows {
		result = append(result, row.ToModel())
	}

	return result, nil
}

func (s *storageImpl) UpdateServer(ctx context.Context, criteria servers.GetCriteria, params servers.UpdateParams) (*servers.Server, error) {
	query := s.stmpBuilder().
		Update(serversTable).
		Set("updated_at", s.now())

	if criteria.ID != nil {
		query = query.Where(sq.Eq{"id": *criteria.ID})
	}

	if params.Name != nil {
		query = query.Set("name", *params.Name)
	}
	if params.UIURL != nil {
		query = query.Set("ui_url", *params.UIURL)
	}
	if params.UIPassword != nil {
		query = query.Set("ui_password", *params.UIPassword)
	}
	if params.CurrentUsers != nil {
		query = query.Set("current_users", *params.CurrentUsers)
	}
	if params.MaxUsers != nil {
		query = query.Set("max_users", *params.MaxUsers)
	}
	if params.Archived != nil {
		query = query.Set("archived", *params.Archived)
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	return s.GetServer(ctx, criteria)
}

// GetAvailableServer returns a server with available capacity (not archived, current_users < max_users)
func (s *storageImpl) GetAvailableServer(ctx context.Context) (*servers.Server, error) {
	query := s.stmpBuilder().
		Select(serverRowFields).
		From(serversTable).
		Where(sq.Eq{"archived": false}).
		Where("current_users < max_users").
		OrderBy("current_users ASC"). // Балансировка: выбираем сервер с минимальной загрузкой
		Limit(1)

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var srv serverRow
	err = s.db.GetContext(ctx, &srv, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return srv.ToModel(), nil
}

// IncrementServerUsers увеличивает счетчик пользователей на сервере
func (s *storageImpl) IncrementServerUsers(ctx context.Context, serverID int64) error {
	q, args, err := s.stmpBuilder().
		Update(serversTable).
		Set("current_users", sq.Expr("current_users + 1")).
		Set("updated_at", s.now()).
		Where(sq.Eq{"id": serverID}).
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

// DecrementServerUsers уменьшает счетчик пользователей на сервере
func (s *storageImpl) DecrementServerUsers(ctx context.Context, serverID int64) error {
	q, args, err := s.stmpBuilder().
		Update(serversTable).
		Set("current_users", sq.Expr("current_users - 1")).
		Set("updated_at", s.now()).
		Where(sq.Eq{"id": serverID}).
		Where("current_users > 0"). // Защита от отрицательных значений
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
