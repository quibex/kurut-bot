package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
)

const wgServersTable = "wg_servers"

type WGServer struct {
	ID            int64     `db:"id"`
	Name          string    `db:"name"`
	Endpoint      string    `db:"endpoint"`
	GRPCAddress   string    `db:"grpc_address"`
	Interface     string    `db:"interface"`
	DNSServers    string    `db:"dns_servers"`
	MaxPeers      int       `db:"max_peers"`
	CurrentPeers  int       `db:"current_peers"`
	Enabled       bool      `db:"enabled"`
	Archived      bool      `db:"archived"`
	TLSEnabled    bool      `db:"tls_enabled"`
	TLSCertPath   *string   `db:"tls_cert_path"`
	TLSServerName *string   `db:"tls_server_name"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

var wgServerRowFields = fields(WGServer{})

func (s *storageImpl) CreateWGServer(ctx context.Context, server WGServer) (*WGServer, error) {
	now := s.now()

	params := map[string]interface{}{
		"name":            server.Name,
		"endpoint":        server.Endpoint,
		"grpc_address":    server.GRPCAddress,
		"interface":       server.Interface,
		"dns_servers":     server.DNSServers,
		"max_peers":       server.MaxPeers,
		"current_peers":   0,
		"enabled":         server.Enabled,
		"archived":        server.Archived,
		"tls_enabled":     server.TLSEnabled,
		"tls_cert_path":   server.TLSCertPath,
		"tls_server_name": server.TLSServerName,
		"created_at":      now,
		"updated_at":      now,
	}

	q, args, err := s.stmpBuilder().
		Insert(wgServersTable).
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

	return s.GetWGServer(ctx, id)
}

func (s *storageImpl) GetWGServer(ctx context.Context, id int64) (*WGServer, error) {
	query := s.stmpBuilder().
		Select(wgServerRowFields).
		From(wgServersTable).
		Where(sq.Eq{"id": id}).
		Limit(1)

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var server WGServer
	err = s.db.GetContext(ctx, &server, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return &server, nil
}

func (s *storageImpl) ListWGServers(ctx context.Context) ([]*WGServer, error) {
	query := s.stmpBuilder().
		Select(wgServerRowFields).
		From(wgServersTable).
		OrderBy("name ASC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var servers []*WGServer
	err = s.db.SelectContext(ctx, &servers, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	return servers, nil
}

func (s *storageImpl) ListEnabledWGServers(ctx context.Context) ([]*WGServer, error) {
	query := s.stmpBuilder().
		Select(wgServerRowFields).
		From(wgServersTable).
		Where(sq.Eq{"enabled": true, "archived": false}).
		OrderBy("current_peers ASC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var servers []*WGServer
	err = s.db.SelectContext(ctx, &servers, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	return servers, nil
}

func (s *storageImpl) UpdateWGServer(ctx context.Context, id int64, params map[string]interface{}) (*WGServer, error) {
	params["updated_at"] = s.now()

	q, args, err := s.stmpBuilder().
		Update(wgServersTable).
		SetMap(params).
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	return s.GetWGServer(ctx, id)
}

func (s *storageImpl) IncrementWGServerPeerCount(ctx context.Context, serverID int64) error {
	q, args, err := s.stmpBuilder().
		Update(wgServersTable).
		Set("current_peers", sq.Expr("current_peers + 1")).
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

func (s *storageImpl) DecrementWGServerPeerCount(ctx context.Context, serverID int64) error {
	q, args, err := s.stmpBuilder().
		Update(wgServersTable).
		Set("current_peers", sq.Expr("MAX(current_peers - 1, 0)")).
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

func (s *storageImpl) ArchiveWGServer(ctx context.Context, id int64) (*WGServer, error) {
	params := map[string]interface{}{
		"archived": true,
	}
	return s.UpdateWGServer(ctx, id, params)
}

func (s *storageImpl) UnarchiveWGServer(ctx context.Context, id int64) (*WGServer, error) {
	params := map[string]interface{}{
		"archived": false,
	}
	return s.UpdateWGServer(ctx, id, params)
}

func (s *storageImpl) DeleteWGServer(ctx context.Context, id int64) error {
	q, args, err := s.stmpBuilder().
		Delete(wgServersTable).
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

