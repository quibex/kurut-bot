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
	ID                  int64      `db:"id"`
	UserID              int64      `db:"user_id"`
	TariffID            int64      `db:"tariff_id"`
	ServerID            *int64     `db:"server_id"`
	Status              string     `db:"status"`
	ClientWhatsApp      *string    `db:"client_whatsapp"`
	GeneratedUserID     *string    `db:"generated_user_id"`
	CreatedByTelegramID *int64     `db:"created_by_telegram_id"`
	ActivatedAt         *time.Time `db:"activated_at"`
	ExpiresAt           *time.Time `db:"expires_at"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
}

func (s subscriptionRow) ToModel() *subs.Subscription {
	return &subs.Subscription{
		ID:                  s.ID,
		UserID:              s.UserID,
		TariffID:            s.TariffID,
		ServerID:            s.ServerID,
		Status:              subs.Status(s.Status),
		ClientWhatsApp:      s.ClientWhatsApp,
		GeneratedUserID:     s.GeneratedUserID,
		CreatedByTelegramID: s.CreatedByTelegramID,
		ActivatedAt:         s.ActivatedAt,
		ExpiresAt:           s.ExpiresAt,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	}
}

func (s *storageImpl) CreateSubscription(ctx context.Context, subscription subs.Subscription) (*subs.Subscription, error) {
	now := s.now()

	params := map[string]interface{}{
		"user_id":                subscription.UserID,
		"tariff_id":              subscription.TariffID,
		"server_id":              subscription.ServerID,
		"status":                 string(subscription.Status),
		"client_whatsapp":        subscription.ClientWhatsApp,
		"generated_user_id":      subscription.GeneratedUserID,
		"created_by_telegram_id": subscription.CreatedByTelegramID,
		"activated_at":           subscription.ActivatedAt,
		"expires_at":             subscription.ExpiresAt,
		"created_at":             now,
		"updated_at":             now,
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
	if criteria.CreatedByTelegramID != nil {
		query = query.Where(sq.Eq{"created_by_telegram_id": *criteria.CreatedByTelegramID})
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

// UpdateSubscriptionGeneratedUserID updates the generated_user_id field
func (s *storageImpl) UpdateSubscriptionGeneratedUserID(ctx context.Context, subscriptionID int64, generatedUserID string) error {
	params := map[string]interface{}{
		"generated_user_id": generatedUserID,
		"updated_at":        s.now(),
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

// ListExpiringTodayGroupedByAssistant returns subscriptions expiring today grouped by assistant telegram ID
func (s *storageImpl) ListExpiringTodayGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error) {
	subscriptions, err := s.ListExpiringSubscriptions(ctx, 0)
	if err != nil {
		return nil, err
	}

	result := make(map[int64][]*subs.Subscription)
	for _, sub := range subscriptions {
		if sub.CreatedByTelegramID != nil {
			result[*sub.CreatedByTelegramID] = append(result[*sub.CreatedByTelegramID], sub)
		}
	}

	return result, nil
}

// ListOverdueSubscriptionsGroupedByAssistant returns expired subscriptions grouped by assistant telegram ID
func (s *storageImpl) ListOverdueSubscriptionsGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error) {
	subscriptions, err := s.ListExpiredSubscriptions(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[int64][]*subs.Subscription)
	for _, sub := range subscriptions {
		if sub.CreatedByTelegramID != nil {
			result[*sub.CreatedByTelegramID] = append(result[*sub.CreatedByTelegramID], sub)
		}
	}

	return result, nil
}

// ListExpiringTomorrowGroupedByAssistant returns subscriptions expiring tomorrow grouped by assistant telegram ID
func (s *storageImpl) ListExpiringTomorrowGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error) {
	subscriptions, err := s.ListExpiringSubscriptions(ctx, 1) // 1 день = завтра
	if err != nil {
		return nil, err
	}

	result := make(map[int64][]*subs.Subscription)
	for _, sub := range subscriptions {
		if sub.CreatedByTelegramID != nil {
			result[*sub.CreatedByTelegramID] = append(result[*sub.CreatedByTelegramID], sub)
		}
	}

	return result, nil
}

// AssistantStats holds statistics for an assistant
type AssistantStats struct {
	TotalActive      int
	CreatedToday     int
	CreatedYesterday int
}

// GetAssistantStats returns subscription statistics for an assistant
func (s *storageImpl) GetAssistantStats(ctx context.Context, assistantTelegramID int64) (*AssistantStats, error) {
	now := s.now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	stats := &AssistantStats{}

	// Count total active subscriptions
	activeQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.Eq{"status": string(subs.StatusActive)})

	q, args, err := activeQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build active count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.TotalActive, q, args...); err != nil {
		return nil, fmt.Errorf("count active: %w", err)
	}

	// Count created today
	todayQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.GtOrEq{"created_at": todayStart})

	q, args, err = todayQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build today count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.CreatedToday, q, args...); err != nil {
		return nil, fmt.Errorf("count today: %w", err)
	}

	// Count created yesterday
	yesterdayQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.GtOrEq{"created_at": yesterdayStart}).
		Where(sq.Lt{"created_at": todayStart})

	q, args, err = yesterdayQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build yesterday count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.CreatedYesterday, q, args...); err != nil {
		return nil, fmt.Errorf("count yesterday: %w", err)
	}

	return stats, nil
}
