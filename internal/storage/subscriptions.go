package storage

import (
	"context"
	"fmt"
	"time"

	"kurut-bot/internal/stories/subs"
)

const subscriptionsTable = "subscriptions"

var subscriptionRowFields = fields(subscriptionRow{})

type subscriptionRow struct {
	ID            int64      `db:"id"`
	UserID        int64      `db:"user_id"`
	TariffID      int64      `db:"tariff_id"`
	MarzbanUserID string     `db:"marzban_user_id"`
	Status        string     `db:"status"`
	ActivatedAt   *time.Time `db:"activated_at"`
	ExpiresAt     *time.Time `db:"expires_at"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

func (s subscriptionRow) ToModel() *subs.Subscription {
	return &subs.Subscription{
		ID:            s.ID,
		UserID:        s.UserID,
		TariffID:      s.TariffID,
		MarzbanUserID: s.MarzbanUserID,
		MarzbanLink:   "", // TODO: добавить поле в БД если нужно
		Status:        subs.Status(s.Status),
		ActivatedAt:   s.ActivatedAt,
		ExpiresAt:     s.ExpiresAt,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func (s *storageImpl) BulkInsertSubscriptions(ctx context.Context, subscriptions []subs.Subscription) ([]subs.Subscription, error) {
	if len(subscriptions) == 0 {
		return []subs.Subscription{}, nil
	}

	query := s.stmpBuilder().
		Insert(subscriptionsTable).
		Columns(fields(subscriptionRow{}))

	now := s.now()
	for _, subscription := range subscriptions {
		query = query.Values(
			subscription.UserID,
			subscription.TariffID,
			subscription.MarzbanUserID,
			string(subscription.Status),
			subscription.ActivatedAt,
			subscription.ExpiresAt,
			now,
			now,
		)
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	// Добавляем RETURNING чтобы получить созданные записи
	q += " RETURNING " + subscriptionRowFields

	var collection []subscriptionRow
	err = s.db.SelectContext(ctx, &collection, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	result := make([]subs.Subscription, 0, len(collection))
	for _, sub := range collection {
		result = append(result, *sub.ToModel())
	}

	return result, nil
}

// func (s *storageImpl) GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error) {
// 	query := s.stmpBuilder().
// 		Select(subscriptionRowFields).
// 		From(subscriptionsTable).
// 		Limit(1)

// 	if len(criteria.IDs) > 0 {
// 		query = query.Where(sq.Eq{"id": criteria.IDs})
// 	}
// 	if len(criteria.UserIDs) > 0 {
// 		query = query.Where(sq.Eq{"user_id": criteria.UserIDs})
// 	}
// 	if len(criteria.MarzbanUserIDs) > 0 {
// 		query = query.Where(sq.Eq{"": *criteria.ID})
// 	}

// 	q, args, err := query.ToSql()
// 	if err != nil {
// 		return nil, fmt.Errorf("build sql query: %w", err)
// 	}

// 	var sub subscriptionRow
// 	err = s.db.GetContext(ctx, &sub, q, args...)
// 	if err != nil {
// 		if err == sql.ErrNoRows {
// 			return nil, nil
// 		}
// 		return nil, fmt.Errorf("db.GetContext: %w", err)
// 	}

// 	return sub.ToModel(), nil
// }

// func (s *storageImpl) UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error) {
// 	query := s.stmpBuilder().
// 		Update(subscriptionsTable).
// 		Set("updated_at", s.now())

// 	// Добавляем условия для обновления
// 	if criteria.ID != nil {
// 		query = query.Where(sq.Eq{"id": *criteria.ID})
// 	}
// 	if criteria.UserID != nil {
// 		query = query.Where(sq.Eq{"user_id": *criteria.UserID})
// 	}
// 	if criteria.MarzbanUserID != nil {
// 		query = query.Where(sq.Eq{"marzban_user_id": *criteria.MarzbanUserID})
// 	}

// 	// Добавляем параметры для обновления
// 	if params.Status != nil {
// 		query = query.Set("status", string(*params.Status))
// 	}
// 	if params.ActivatedAt != nil {
// 		query = query.Set("activated_at", *params.ActivatedAt)
// 	}
// 	if params.ExpiresAt != nil {
// 		query = query.Set("expires_at", *params.ExpiresAt)
// 	}

// 	q, args, err := query.ToSql()
// 	if err != nil {
// 		return nil, fmt.Errorf("build sql query: %w", err)
// 	}

// 	_, err = s.db.ExecContext(ctx, q, args...)
// 	if err != nil {
// 		return nil, fmt.Errorf("db.ExecContext: %w", err)
// 	}

// 	return s.GetSubscription(ctx, criteria)
// }

// func (s *storageImpl) ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error) {
// 	query := s.stmpBuilder().
// 		Select(subscriptionRowFields).
// 		From(subscriptionsTable)

// 	if criteria.UserID != nil {
// 		query = query.Where(sq.Eq{"user_id": *criteria.UserID})
// 	}
// 	if criteria.TariffID != nil {
// 		query = query.Where(sq.Eq{"tariff_id": *criteria.TariffID})
// 	}
// 	if criteria.Status != nil {
// 		query = query.Where(sq.Eq{"status": string(*criteria.Status)})
// 	}

// 	if criteria.Limit > 0 {
// 		query = query.Limit(uint64(criteria.Limit))
// 	}
// 	if criteria.Offset > 0 {
// 		query = query.Offset(uint64(criteria.Offset))
// 	}

// 	query = query.OrderBy("created_at DESC")

// 	q, args, err := query.ToSql()
// 	if err != nil {
// 		return nil, fmt.Errorf("build sql query: %w", err)
// 	}

// 	var collection []subscriptionRow
// 	err = s.db.SelectContext(ctx, &collection, q, args...)
// 	if err != nil {
// 		return nil, fmt.Errorf("db.SelectContext: %w", err)
// 	}

// 	result := make([]*subs.Subscription, 0, len(collection))
// 	for _, sub := range collection {
// 		result = append(result, sub.ToModel())
// 	}

// 	return result, nil
// }

// func (s *storageImpl) DeleteSubscription(ctx context.Context, criteria subs.DeleteCriteria) error {
// 	query := s.stmpBuilder().Delete(subscriptionsTable)

// 	if criteria.ID != nil {
// 		query = query.Where(sq.Eq{"id": *criteria.ID})
// 	}
// 	if criteria.UserID != nil {
// 		query = query.Where(sq.Eq{"user_id": *criteria.UserID})
// 	}
// 	if criteria.MarzbanUserID != nil {
// 		query = query.Where(sq.Eq{"marzban_user_id": *criteria.MarzbanUserID})
// 	}

// 	q, args, err := query.ToSql()
// 	if err != nil {
// 		return fmt.Errorf("build sql query: %w", err)
// 	}

// 	_, err = s.db.ExecContext(ctx, q, args...)
// 	if err != nil {
// 		return fmt.Errorf("db.ExecContext: %w", err)
// 	}

// 	return nil
// }
