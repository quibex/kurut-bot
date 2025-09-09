package sqlite3

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var (
	defaultConnTimeout     = 10 * time.Second
	defaultMaxOpenConns    = 100
	defaultMaxIdleConns    = 10
	defaultConnMaxLifetime = time.Hour
)

type config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnTimeout     time.Duration
}

type Option func(*config)

func WithDSN(dsn string) Option {
	return func(c *config) {
		c.DSN = dsn
	}
}

func WithMaxOpenConns(maxOpen int) Option {
	return func(c *config) {
		c.MaxOpenConns = maxOpen
	}
}

func WithMaxIdleConns(maxIdle int) Option {
	return func(c *config) {
		c.MaxIdleConns = maxIdle
	}
}

func WithConnMaxLifetime(lifetime time.Duration) Option {
	return func(c *config) {
		c.ConnMaxLifetime = lifetime
	}
}

func WithConnTimeout(timeout time.Duration) Option {
	return func(c *config) {
		c.ConnTimeout = timeout
	}
}

func newConfig(opts ...Option) *config {
	cfg := &config{
		DSN:             ":memory:",
		MaxOpenConns:    defaultMaxOpenConns,
		MaxIdleConns:    defaultMaxIdleConns,
		ConnMaxLifetime: defaultConnMaxLifetime,
		ConnTimeout:     defaultConnTimeout,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

func New(ctx context.Context, opts ...Option) (*DB, error) {
	cfg := newConfig(opts...)

	db, err := sqlx.Open("sqlite3", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite3 database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(ctx, cfg.ConnTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping SQLite3 database: %w", err)
	}

	return &DB{
		DB: db,
	}, nil
}

type DB struct {
	*sqlx.DB
}

func (d *DB) Close() error {
	return d.DB.Close()
}

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return d.DB.BeginTx(ctx, opts)
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return d.DB.ExecContext(ctx, query, args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, query, args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return d.DB.QueryRowContext(ctx, query, args...)
}
