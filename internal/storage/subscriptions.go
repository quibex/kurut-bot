package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	ReferrerWhatsApp    *string    `db:"referrer_whatsapp"`
	ActivatedAt         *time.Time `db:"activated_at"`
	ExpiresAt           *time.Time `db:"expires_at"`
	LastRenewedAt       *time.Time `db:"last_renewed_at"`
	RenewalCount        int        `db:"renewal_count"`
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
		ReferrerWhatsApp:    s.ReferrerWhatsApp,
		ActivatedAt:         s.ActivatedAt,
		ExpiresAt:           s.ExpiresAt,
		LastRenewedAt:       s.LastRenewedAt,
		RenewalCount:        s.RenewalCount,
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
		"referrer_whatsapp":      subscription.ReferrerWhatsApp,
		"activated_at":           subscription.ActivatedAt,
		"expires_at":             subscription.ExpiresAt,
		"last_renewed_at":        now,
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

	// Update subscription with incremented renewal_count
	now := s.now()

	q, args, err := s.stmpBuilder().
		Update(subscriptionsTable).
		Set("expires_at", newExpiresAt).
		Set("last_renewed_at", now).
		Set("renewal_count", sq.Expr("renewal_count + 1")).
		Set("updated_at", now).
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

// ListExpiringByAssistantAndDays returns subscriptions expiring in N days grouped by assistant telegram ID
func (s *storageImpl) ListExpiringByAssistantAndDays(ctx context.Context, daysUntilExpiry int) (map[int64][]*subs.Subscription, error) {
	subscriptions, err := s.ListExpiringSubscriptions(ctx, daysUntilExpiry)
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
	now := s.now()

	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Where(sq.Eq{"status": string(subs.StatusExpired)}).
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

	result := make(map[int64][]*subs.Subscription)
	for _, row := range rows {
		sub := row.ToModel()
		if sub.CreatedByTelegramID != nil {
			result[*sub.CreatedByTelegramID] = append(result[*sub.CreatedByTelegramID], sub)
		}
	}

	return result, nil
}

// ListStaleExpiredSubscriptionsGroupedByAssistant returns expired subscriptions that have been expired for more than 24 hours
// These are subscriptions that need to be disabled but haven't been yet
func (s *storageImpl) ListStaleExpiredSubscriptionsGroupedByAssistant(ctx context.Context) (map[int64][]*subs.Subscription, error) {
	now := s.now()
	staleThreshold := now.Add(-24 * time.Hour) // expired more than 24 hours ago

	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Where(sq.Eq{"status": string(subs.StatusExpired)}).
		Where(sq.Lt{"expires_at": staleThreshold}).
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

	result := make(map[int64][]*subs.Subscription)
	for _, row := range rows {
		sub := row.ToModel()
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
	CreatedThisWeek  int
	CreatedLastWeek  int
}

// GetAssistantStats returns subscription statistics for an assistant
func (s *storageImpl) GetAssistantStats(ctx context.Context, assistantTelegramID int64) (*AssistantStats, error) {
	now := s.now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	// Calculate this week start (Monday)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday is 7
	}
	thisWeekStart := todayStart.AddDate(0, 0, -(weekday - 1))
	lastWeekStart := thisWeekStart.AddDate(0, 0, -7)

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

	// Count renewed/created today (uses last_renewed_at to include renewals)
	todayQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.GtOrEq{"last_renewed_at": todayStart})

	q, args, err = todayQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build today count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.CreatedToday, q, args...); err != nil {
		return nil, fmt.Errorf("count today: %w", err)
	}

	// Count renewed/created yesterday
	yesterdayQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.GtOrEq{"last_renewed_at": yesterdayStart}).
		Where(sq.Lt{"last_renewed_at": todayStart})

	q, args, err = yesterdayQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build yesterday count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.CreatedYesterday, q, args...); err != nil {
		return nil, fmt.Errorf("count yesterday: %w", err)
	}

	// Count renewed/created this week (Monday to now)
	thisWeekQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.GtOrEq{"last_renewed_at": thisWeekStart})

	q, args, err = thisWeekQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build this week count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.CreatedThisWeek, q, args...); err != nil {
		return nil, fmt.Errorf("count this week: %w", err)
	}

	// Count renewed/created last week (Monday to Sunday)
	lastWeekQuery := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"created_by_telegram_id": assistantTelegramID}).
		Where(sq.GtOrEq{"last_renewed_at": lastWeekStart}).
		Where(sq.Lt{"last_renewed_at": thisWeekStart})

	q, args, err = lastWeekQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build last week count query: %w", err)
	}
	if err := s.db.GetContext(ctx, &stats.CreatedLastWeek, q, args...); err != nil {
		return nil, fmt.Errorf("count last week: %w", err)
	}

	return stats, nil
}

// ListExpiringSubscriptionsByAssistant returns expiring subscriptions for a specific assistant
// If assistantTelegramID is nil, returns all expiring subscriptions (for admins)
func (s *storageImpl) ListExpiringSubscriptionsByAssistant(ctx context.Context, assistantTelegramID *int64, daysUntilExpiry int) ([]*subs.Subscription, error) {
	startTime := s.now().AddDate(0, 0, daysUntilExpiry)
	endTime := startTime.Add(24 * time.Hour)

	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Where(sq.Eq{"status": string(subs.StatusActive)}).
		Where(sq.GtOrEq{"expires_at": startTime}).
		Where(sq.Lt{"expires_at": endTime}).
		OrderBy("expires_at ASC")

	if assistantTelegramID != nil {
		query = query.Where(sq.Eq{"created_by_telegram_id": *assistantTelegramID})
	}

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

// ListExpiredSubscriptionsByAssistant returns expired subscriptions for a specific assistant
// If assistantTelegramID is nil, returns all expired subscriptions (for admins)
func (s *storageImpl) ListExpiredSubscriptionsByAssistant(ctx context.Context, assistantTelegramID *int64) ([]*subs.Subscription, error) {
	now := s.now()

	query := s.stmpBuilder().
		Select(subscriptionRowFields).
		From(subscriptionsTable).
		Where(sq.Eq{"status": string(subs.StatusExpired)}).
		Where(sq.Lt{"expires_at": now}).
		OrderBy("expires_at ASC")

	if assistantTelegramID != nil {
		query = query.Where(sq.Eq{"created_by_telegram_id": *assistantTelegramID})
	}

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

// UpdateSubscriptionTariff updates the tariff for a subscription
func (s *storageImpl) UpdateSubscriptionTariff(ctx context.Context, subscriptionID int64, tariffID int64) error {
	params := map[string]interface{}{
		"tariff_id":  tariffID,
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

// FindActiveSubscriptionByWhatsApp finds an active subscription by client WhatsApp number
func (s *storageImpl) FindActiveSubscriptionByWhatsApp(ctx context.Context, whatsapp string) (*subs.Subscription, error) {
	normalized := NormalizePhone(whatsapp)

	query := `
		SELECT ` + subscriptionRowFields + `
		FROM ` + subscriptionsTable + `
		WHERE REPLACE(REPLACE(REPLACE(client_whatsapp, '+', ''), ' ', ''), '-', '') = ?
		AND status = ?
		ORDER BY expires_at DESC
		LIMIT 1
	`

	var sub subscriptionRow
	err := s.db.GetContext(ctx, &sub, query, normalized, string(subs.StatusActive))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetContext: %w", err)
	}

	return sub.ToModel(), nil
}

// NormalizePhone returns only digits from phone number
func NormalizePhone(phone string) string {
	var result strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// HasUsedTrialByPhone checks if client has used trial by phone number
func (s *storageImpl) HasUsedTrialByPhone(ctx context.Context, phoneNumber string) (bool, error) {
	normalized := NormalizePhone(phoneNumber)

	query := `
		SELECT COUNT(*)
		FROM subscriptions
		WHERE REPLACE(REPLACE(REPLACE(client_whatsapp, '+', ''), ' ', ''), '-', '') = ?
	`

	var count int
	err := s.db.GetContext(ctx, &count, query, normalized)
	if err != nil {
		return false, fmt.Errorf("db.GetContext: %w", err)
	}

	return count > 0, nil
}

// HasPaidSubscriptionByPhone checks if client has any paid subscription by phone number
func (s *storageImpl) HasPaidSubscriptionByPhone(ctx context.Context, phoneNumber string) (bool, error) {
	normalized := NormalizePhone(phoneNumber)

	query := `
		SELECT COUNT(*)
		FROM subscriptions s
		INNER JOIN payment_subscriptions ps ON s.id = ps.subscription_id
		WHERE REPLACE(REPLACE(REPLACE(s.client_whatsapp, '+', ''), ' ', ''), '-', '') = ?
	`

	var count int
	err := s.db.GetContext(ctx, &count, query, normalized)
	if err != nil {
		return false, fmt.Errorf("db.GetContext: %w", err)
	}

	return count > 0, nil
}

// CountWeeklyReferrals counts how many people were invited by referrerWhatsApp this week
func (s *storageImpl) CountWeeklyReferrals(ctx context.Context, referrerWhatsApp string) (int, error) {
	now := s.now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday is 7
	}
	weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(weekday - 1))

	query := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"referrer_whatsapp": referrerWhatsApp}).
		Where(sq.GtOrEq{"created_at": weekStart})

	q, args, err := query.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build sql query: %w", err)
	}

	var count int
	err = s.db.GetContext(ctx, &count, q, args...)
	if err != nil {
		return 0, fmt.Errorf("db.GetContext: %w", err)
	}

	return count, nil
}

// ReferrerStats holds referral statistics
type ReferrerStats struct {
	ReferrerWhatsApp string
	Count            int
}

// GetTopReferrersThisWeek returns top N referrers by invitation count this week
func (s *storageImpl) GetTopReferrersThisWeek(ctx context.Context, limit int) ([]ReferrerStats, error) {
	now := s.now()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday is 7
	}
	weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(weekday - 1))

	query := s.stmpBuilder().
		Select("referrer_whatsapp", "COUNT(*) as count").
		From(subscriptionsTable).
		Where(sq.NotEq{"referrer_whatsapp": nil}).
		Where(sq.NotEq{"referrer_whatsapp": ""}).
		Where(sq.GtOrEq{"created_at": weekStart}).
		GroupBy("referrer_whatsapp").
		OrderBy("count DESC").
		Limit(uint64(limit))

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.QueryContext: %w", err)
	}
	defer rows.Close()

	var result []ReferrerStats
	for rows.Next() {
		var stat ReferrerStats
		if err := rows.Scan(&stat.ReferrerWhatsApp, &stat.Count); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, stat)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}
