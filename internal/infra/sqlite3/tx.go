package sqlite3

import (
	"context"
	"database/sql"
	"fmt"
)

type (
	TxFunc               = func(*sql.Tx) (any, error)
	TxSessionFactoryFunc = func() (*sql.DB, error)
	TxManager            = func(ctx context.Context, fn TxFunc) error
	TxOptions            = *sql.TxOptions
)

func WithTx(starterFn TxSessionFactoryFunc, txOpts TxOptions) TxManager {
	return func(ctx context.Context, fn TxFunc) error {
		db, err := starterFn()
		if err != nil {
			return fmt.Errorf("db connection error: %w", err)
		}

		tx, err := db.BeginTx(ctx, txOpts)
		if err != nil {
			return fmt.Errorf("db begin transaction: %w", err)
		}
		defer func() {
			if p := recover(); p != nil {
				_ = tx.Rollback() // Ignore rollback error during panic
				panic(p)
			}
		}()

		result, err := fn(tx)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return fmt.Errorf("db transaction error: %v, rollback error: %w", err, rbErr)
			}
			return fmt.Errorf("db transaction error: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("db commit transaction: %w", err)
		}

		_ = result // результат можно использовать при необходимости
		return nil
	}
}
