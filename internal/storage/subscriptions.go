package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kurut-bot/internal/stories/subs"

	sq "github.com/Masterminds/squirrel"
)

const subscriptionsTable = "subscriptions"

var subscriptionRowFields = fields(subscriptionRow{})

type subscriptionRow struct {
	ID            int64      `db:"id"`
	UserID        int64      `db:"user_id"`
	TariffID      int64      `db:"tariff_id"`
	MarzbanUserID string     `db:"marzban_user_id"`
	MarzbanLink   string     `db:"marzban_link"`
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
		MarzbanLink:   s.MarzbanLink,
		Status:        subs.Status(s.Status),
		ActivatedAt:   s.ActivatedAt,
		ExpiresAt:     s.ExpiresAt,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func (s *storageImpl) CreateSubscription(ctx context.Context, subscription subs.Subscription) (*subs.Subscription, error) {
	now := s.now()

	params := map[string]interface{}{
		"user_id":         subscription.UserID,
		"tariff_id":       subscription.TariffID,
		"marzban_user_id": subscription.MarzbanUserID,
		"marzban_link":    subscription.MarzbanLink,
		"status":          string(subscription.Status),
		"activated_at":    subscription.ActivatedAt,
		"expires_at":      subscription.ExpiresAt,
		"created_at":      now,
		"updated_at":      now,
	}

	q, args, err := s.stmpBuilder().
		Insert(subscriptionsTable).
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

	return s.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{id}})
}

func (s *storageImpl) GetSubscription(ctx context.Context, criteria subs.GetCriteria) (*subs.Subscription, error) {
	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Limit(1)

	if len(criteria.IDs) > 0 {
		query = query.Where(sq.Eq{"id": criteria.IDs})
	}
	if len(criteria.UserIDs) > 0 {
		query = query.Where(sq.Eq{"user_id": criteria.UserIDs})
	}
	if len(criteria.MarzbanUserIDs) > 0 {
		query = query.Where(sq.Eq{"marzban_user_id": criteria.MarzbanUserIDs})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var sub subscriptionRow
	err = s.db.GetContext(ctx, &sub, q, args...)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return sub.ToModel(), nil
}

func (s *storageImpl) ListSubscriptions(ctx context.Context, criteria subs.ListCriteria) ([]*subs.Subscription, error) {
	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable)

	if len(criteria.UserIDs) > 0 {
		query = query.Where(sq.Eq{"user_id": criteria.UserIDs})
	}
	if len(criteria.TariffIDs) > 0 {
		query = query.Where(sq.Eq{"tariff_id": criteria.TariffIDs})
	}
	if len(criteria.Status) > 0 {
		query = query.Where(sq.Eq{"status": criteria.Status})
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

	var rows []subscriptionRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	var subscriptions []*subs.Subscription
	for _, row := range rows {
		subscriptions = append(subscriptions, row.ToModel())
	}

	return subscriptions, nil
}

// ListExpiringSubscriptions returns active subscriptions expiring in specified number of days
func (s *storageImpl) ListExpiringSubscriptions(ctx context.Context, daysUntilExpiry int) ([]*subs.Subscription, error) {
	// Calculate time window: from now+days to now+days+24h
	startTime := s.now().AddDate(0, 0, daysUntilExpiry)
	endTime := startTime.Add(24 * time.Hour)

	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Where(sq.Eq{"status": string(subs.StatusActive)}).
		Where(sq.GtOrEq{"expires_at": startTime}).
		Where(sq.Lt{"expires_at": endTime}).
		OrderBy("expires_at ASC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var rows []subscriptionRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	var subscriptions []*subs.Subscription
	for _, row := range rows {
		subscriptions = append(subscriptions, row.ToModel())
	}

	return subscriptions, nil
}

// ListExpiredSubscriptions returns active subscriptions that have expired
func (s *storageImpl) ListExpiredSubscriptions(ctx context.Context) ([]*subs.Subscription, error) {
	now := s.now()

	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Where(sq.Eq{"status": string(subs.StatusActive)}).
		Where(sq.Lt{"expires_at": now}).
		OrderBy("expires_at ASC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var rows []subscriptionRow
	err = s.db.SelectContext(ctx, &rows, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	var subscriptions []*subs.Subscription
	for _, row := range rows {
		subscriptions = append(subscriptions, row.ToModel())
	}

	return subscriptions, nil
}

// ExtendSubscription extends subscription by adding days to expires_at
func (s *storageImpl) ExtendSubscription(ctx context.Context, subscriptionID int64, additionalDays int) error {
	// First, get the current subscription to get expires_at
	criteria := subs.GetCriteria{IDs: []int64{subscriptionID}}
	subscription, err := s.GetSubscription(ctx, criteria)
	if err != nil {
		return fmt.Errorf("get subscription: %w", err)
	}
	if subscription == nil {
		return fmt.Errorf("subscription not found: %d", subscriptionID)
	}

	// Calculate new expiration date
	var newExpiresAt time.Time
	if subscription.ExpiresAt != nil {
		newExpiresAt = subscription.ExpiresAt.AddDate(0, 0, additionalDays)
	} else {
		// If no expiration set, start from now
		newExpiresAt = s.now().AddDate(0, 0, additionalDays)
	}

	// Update subscription
	params := map[string]interface{}{
		"expires_at": newExpiresAt,
		"updated_at": s.now(),
	}

	q, args, err := s.stmpBuilder().
		Update(subscriptionsTable).
		SetMap(params).
		Where(sq.Eq{"id": subscriptionID}).
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

// UpdateSubscription updates subscription fields based on criteria
func (s *storageImpl) UpdateSubscription(ctx context.Context, criteria subs.GetCriteria, params subs.UpdateParams) (*subs.Subscription, error) {
	updateMap := map[string]interface{}{
		"updated_at": s.now(),
	}

	if params.Status != nil {
		updateMap["status"] = string(*params.Status)
	}
	if params.ActivatedAt != nil {
		updateMap["activated_at"] = *params.ActivatedAt
	}
	if params.ExpiresAt != nil {
		updateMap["expires_at"] = *params.ExpiresAt
	}

	query := s.stmpBuilder().
		Update(subscriptionsTable).
		SetMap(updateMap)

	if len(criteria.IDs) > 0 {
		query = query.Where(sq.Eq{"id": criteria.IDs})
	}
	if len(criteria.UserIDs) > 0 {
		query = query.Where(sq.Eq{"user_id": criteria.UserIDs})
	}
	if len(criteria.MarzbanUserIDs) > 0 {
		query = query.Where(sq.Eq{"marzban_user_id": criteria.MarzbanUserIDs})
	}

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	_, err = s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ExecContext: %w", err)
	}

	return s.GetSubscription(ctx, criteria)
}
