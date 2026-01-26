package storage

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
)

type TariffStats struct {
	TariffID   int64  `db:"tariff_id"`
	TariffName string `db:"tariff_name"`
	UserCount  int    `db:"user_count"`
}

type StatisticsData struct {
	ActiveSubscriptionsCount int
	ActiveUsersCount         int
	InactiveUsersCount       int
	ActiveTariffStats        []TariffStats
	ArchivedTariffStats      []TariffStats
	ArchivedTariffUsersCount int
	PreviousMonthRevenue     float64
	CurrentMonthRevenue      float64
	TodayRevenue             float64
	YesterdayRevenue         float64
	AverageRevenuePerDay     float64
}

func (s *storageImpl) GetActiveSubscriptionsCount(ctx context.Context) (int, error) {
	query := s.stmpBuilder().
		Select("COUNT(*)").
		From(subscriptionsTable).
		Where(sq.Eq{"status": "active"})

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

func (s *storageImpl) GetActiveUsersCount(ctx context.Context) (int, error) {
	query := s.stmpBuilder().
		Select("COUNT(DISTINCT user_id)").
		From(subscriptionsTable).
		Where(sq.Eq{"status": "active"})

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

func (s *storageImpl) GetInactiveUsersCount(ctx context.Context) (int, error) {
	query := s.stmpBuilder().
		Select("COUNT(DISTINCT u.id)").
		From(usersTable + " u").
		LeftJoin(subscriptionsTable + " s ON u.id = s.user_id AND s.status = 'active'").
		Where("s.id IS NULL")

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

func (s *storageImpl) GetActiveTariffStatistics(ctx context.Context) ([]TariffStats, error) {
	query := s.stmpBuilder().
		Select("t.id as tariff_id", "t.name as tariff_name", "COUNT(s.id) as user_count").
		From(tariffsTable+" t").
		LeftJoin(subscriptionsTable+" s ON t.id = s.tariff_id AND s.status = 'active'").
		Where(sq.Eq{"t.is_active": true}).
		GroupBy("t.id", "t.name").
		OrderBy("t.duration_days ASC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var stats []TariffStats
	err = s.db.SelectContext(ctx, &stats, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	return stats, nil
}

func (s *storageImpl) GetArchivedTariffStatistics(ctx context.Context) ([]TariffStats, error) {
	query := s.stmpBuilder().
		Select("t.id as tariff_id", "t.name as tariff_name", "COUNT(s.id) as user_count").
		From(tariffsTable+" t").
		LeftJoin(subscriptionsTable+" s ON t.id = s.tariff_id AND s.status = 'active'").
		Where(sq.Eq{"t.is_active": false}).
		GroupBy("t.id", "t.name").
		OrderBy("user_count DESC")

	q, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build sql query: %w", err)
	}

	var stats []TariffStats
	err = s.db.SelectContext(ctx, &stats, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db.SelectContext: %w", err)
	}

	return stats, nil
}

func (s *storageImpl) GetRevenueForMonth(ctx context.Context, year int, month time.Month) (float64, error) {
	startDate := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 1, 0)

	query := s.stmpBuilder().
		Select("COALESCE(SUM(amount), 0)").
		From(paymentsTable).
		Where(sq.Eq{"status": "approved"}).
		Where(sq.GtOrEq{"created_at": startDate}).
		Where(sq.Lt{"created_at": endDate})

	q, args, err := query.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build sql query: %w", err)
	}

	var revenue float64
	err = s.db.GetContext(ctx, &revenue, q, args...)
	if err != nil {
		return 0, fmt.Errorf("db.GetContext: %w", err)
	}

	return revenue, nil
}

func (s *storageImpl) GetRevenueForDay(ctx context.Context, date time.Time) (float64, error) {
	startDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 0, 1)

	query := s.stmpBuilder().
		Select("COALESCE(SUM(amount), 0)").
		From(paymentsTable).
		Where(sq.Eq{"status": "approved"}).
		Where(sq.GtOrEq{"created_at": startDate}).
		Where(sq.Lt{"created_at": endDate})

	q, args, err := query.ToSql()
	if err != nil {
		return 0, fmt.Errorf("build sql query: %w", err)
	}

	var revenue float64
	err = s.db.GetContext(ctx, &revenue, q, args...)
	if err != nil {
		return 0, fmt.Errorf("db.GetContext: %w", err)
	}

	return revenue, nil
}

func (s *storageImpl) GetStatistics(ctx context.Context) (*StatisticsData, error) {
	now := s.now()
	currentYear, currentMonth, _ := now.Date()
	previousMonth := currentMonth - 1
	previousYear := currentYear
	if previousMonth == 0 {
		previousMonth = 12
		previousYear--
	}

	activeSubsCount, err := s.GetActiveSubscriptionsCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active subscriptions count: %w", err)
	}

	activeUsersCount, err := s.GetActiveUsersCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active users count: %w", err)
	}

	inactiveUsersCount, err := s.GetInactiveUsersCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("get inactive users count: %w", err)
	}

	activeTariffStats, err := s.GetActiveTariffStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active tariff statistics: %w", err)
	}

	archivedTariffStats, err := s.GetArchivedTariffStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("get archived tariff statistics: %w", err)
	}

	archivedTariffUsersCount := 0
	for _, stat := range archivedTariffStats {
		archivedTariffUsersCount += stat.UserCount
	}

	previousMonthRevenue, err := s.GetRevenueForMonth(ctx, previousYear, previousMonth)
	if err != nil {
		return nil, fmt.Errorf("get previous month revenue: %w", err)
	}

	currentMonthRevenue, err := s.GetRevenueForMonth(ctx, currentYear, currentMonth)
	if err != nil {
		return nil, fmt.Errorf("get current month revenue: %w", err)
	}

	todayRevenue, err := s.GetRevenueForDay(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("get today revenue: %w", err)
	}

	yesterday := now.AddDate(0, 0, -1)
	yesterdayRevenue, err := s.GetRevenueForDay(ctx, yesterday)
	if err != nil {
		return nil, fmt.Errorf("get yesterday revenue: %w", err)
	}

	averageRevenuePerDay := 0.0
	daysInMonth := float64(now.Day())
	if daysInMonth > 0 {
		averageRevenuePerDay = currentMonthRevenue / daysInMonth
	}

	return &StatisticsData{
		ActiveSubscriptionsCount: activeSubsCount,
		ActiveUsersCount:         activeUsersCount,
		InactiveUsersCount:       inactiveUsersCount,
		ActiveTariffStats:        activeTariffStats,
		ArchivedTariffStats:      archivedTariffStats,
		ArchivedTariffUsersCount: archivedTariffUsersCount,
		PreviousMonthRevenue:     previousMonthRevenue,
		CurrentMonthRevenue:      currentMonthRevenue,
		TodayRevenue:             todayRevenue,
		YesterdayRevenue:         yesterdayRevenue,
		AverageRevenuePerDay:     averageRevenuePerDay,
	}, nil
}

// CustomerAnalytics contains customer analytics data
type CustomerAnalytics struct {
	NewCustomersThisWeek  int
	NewCustomersLastWeek  int
	NewCustomersThisMonth int
	NewCustomersLastMonth int
	WeekOverWeekGrowth    float64
	MonthOverMonthGrowth  float64

	RenewalRate  float64
	ChurnRate    float64
	RenewedCount int // subscriptions with renewal_count > 0
	ChurnedCount int // expired subscriptions without renewal
	TotalMature  int // total subscriptions created 30+ days ago

	ARPU                float64
	TrialConversionRate float64
	RevenueByTariff     []TariffRevenue
}

// TariffRevenue represents revenue data for a specific tariff
type TariffRevenue struct {
	TariffName string
	Revenue    float64
	Count      int
}

// GetCustomerAnalytics returns aggregated customer analytics
func (s *storageImpl) GetCustomerAnalytics(ctx context.Context) (*CustomerAnalytics, error) {
	now := s.now()

	// Calculate time boundaries
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Week calculations (Monday-based)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	thisWeekStart := todayStart.AddDate(0, 0, -(weekday - 1))
	lastWeekStart := thisWeekStart.AddDate(0, 0, -7)

	// Month calculations
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastMonthStart := thisMonthStart.AddDate(0, -1, 0)

	analytics := &CustomerAnalytics{}

	// Get new customers counts
	var err error
	analytics.NewCustomersThisWeek, err = s.GetNewCustomersCount(ctx, thisWeekStart, now)
	if err != nil {
		return nil, fmt.Errorf("get new customers this week: %w", err)
	}

	analytics.NewCustomersLastWeek, err = s.GetNewCustomersCount(ctx, lastWeekStart, thisWeekStart)
	if err != nil {
		return nil, fmt.Errorf("get new customers last week: %w", err)
	}

	analytics.NewCustomersThisMonth, err = s.GetNewCustomersCount(ctx, thisMonthStart, now)
	if err != nil {
		return nil, fmt.Errorf("get new customers this month: %w", err)
	}

	analytics.NewCustomersLastMonth, err = s.GetNewCustomersCount(ctx, lastMonthStart, thisMonthStart)
	if err != nil {
		return nil, fmt.Errorf("get new customers last month: %w", err)
	}

	// Calculate growth rates
	if analytics.NewCustomersLastWeek > 0 {
		analytics.WeekOverWeekGrowth = float64(analytics.NewCustomersThisWeek-analytics.NewCustomersLastWeek) / float64(analytics.NewCustomersLastWeek) * 100
	}
	if analytics.NewCustomersLastMonth > 0 {
		analytics.MonthOverMonthGrowth = float64(analytics.NewCustomersThisMonth-analytics.NewCustomersLastMonth) / float64(analytics.NewCustomersLastMonth) * 100
	}

	// Get renewal and churn stats
	analytics.RenewedCount, analytics.ChurnedCount, analytics.TotalMature, err = s.GetRenewalAndChurnStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get renewal stats: %w", err)
	}

	if analytics.TotalMature > 0 {
		analytics.RenewalRate = float64(analytics.RenewedCount) / float64(analytics.TotalMature) * 100
		analytics.ChurnRate = float64(analytics.ChurnedCount) / float64(analytics.TotalMature) * 100
	}

	// Get ARPU
	analytics.ARPU, err = s.GetARPU(ctx, thisMonthStart, now)
	if err != nil {
		return nil, fmt.Errorf("get ARPU: %w", err)
	}

	// Get trial conversion rate
	analytics.TrialConversionRate, err = s.GetTrialConversionRate(ctx)
	if err != nil {
		return nil, fmt.Errorf("get trial conversion rate: %w", err)
	}

	return analytics, nil
}

// GetNewCustomersCount returns count of new customers (first paid subscription) in the given period
func (s *storageImpl) GetNewCustomersCount(ctx context.Context, start, end time.Time) (int, error) {
	query := `
		SELECT COUNT(DISTINCT s.client_whatsapp)
		FROM subscriptions s
		JOIN payment_subscriptions ps ON s.id = ps.subscription_id
		JOIN payments p ON ps.payment_id = p.id
		WHERE p.status = 'approved'
		  AND s.created_at >= ? AND s.created_at < ?
		  AND s.client_whatsapp NOT IN (
			SELECT DISTINCT s2.client_whatsapp
			FROM subscriptions s2
			JOIN payment_subscriptions ps2 ON s2.id = ps2.subscription_id
			JOIN payments p2 ON ps2.payment_id = p2.id
			WHERE p2.status = 'approved' AND s2.created_at < ?
		  )
	`

	var count int
	err := s.db.GetContext(ctx, &count, query, start, end, start)
	if err != nil {
		return 0, fmt.Errorf("db.GetContext: %w", err)
	}

	return count, nil
}

// GetRenewalAndChurnStats returns renewal and churn statistics for mature subscriptions (30+ days old)
func (s *storageImpl) GetRenewalAndChurnStats(ctx context.Context) (renewed, churned, total int, err error) {
	now := s.now()
	matureDate := now.AddDate(0, 0, -30)

	query := `
		SELECT
			COUNT(CASE WHEN s.renewal_count > 0 THEN 1 END) as renewed,
			COUNT(CASE WHEN s.status = 'expired' AND s.renewal_count = 0 THEN 1 END) as churned,
			COUNT(*) as total
		FROM subscriptions s
		JOIN payment_subscriptions ps ON s.id = ps.subscription_id
		JOIN payments p ON ps.payment_id = p.id
		WHERE p.status = 'approved'
		  AND s.created_at < ?
	`

	var result struct {
		Renewed int `db:"renewed"`
		Churned int `db:"churned"`
		Total   int `db:"total"`
	}

	err = s.db.GetContext(ctx, &result, query, matureDate)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("db.GetContext: %w", err)
	}

	return result.Renewed, result.Churned, result.Total, nil
}

// GetARPU returns average revenue per user for the given period
func (s *storageImpl) GetARPU(ctx context.Context, start, end time.Time) (float64, error) {
	query := `
		SELECT COALESCE(SUM(p.amount), 0) / NULLIF(COUNT(DISTINCT s.client_whatsapp), 0)
		FROM payments p
		JOIN payment_subscriptions ps ON p.id = ps.payment_id
		JOIN subscriptions s ON ps.subscription_id = s.id
		WHERE p.status = 'approved'
		  AND p.created_at >= ? AND p.created_at < ?
	`

	var arpu *float64
	err := s.db.GetContext(ctx, &arpu, query, start, end)
	if err != nil {
		return 0, fmt.Errorf("db.GetContext: %w", err)
	}

	if arpu == nil {
		return 0, nil
	}

	return *arpu, nil
}

// GetTrialConversionRate returns percentage of trial users who converted to paid
func (s *storageImpl) GetTrialConversionRate(ctx context.Context) (float64, error) {
	query := `
		WITH trial_clients AS (
			SELECT DISTINCT s.client_whatsapp
			FROM subscriptions s
			JOIN tariffs t ON s.tariff_id = t.id
			WHERE t.price = 0
		),
		paid_clients AS (
			SELECT DISTINCT s.client_whatsapp
			FROM subscriptions s
			JOIN payment_subscriptions ps ON s.id = ps.subscription_id
			JOIN payments p ON ps.payment_id = p.id
			WHERE p.status = 'approved'
			  AND s.client_whatsapp IN (SELECT client_whatsapp FROM trial_clients)
		)
		SELECT COALESCE(
			CAST(COUNT(*) AS REAL) * 100.0 / NULLIF((SELECT COUNT(*) FROM trial_clients), 0),
			0
		)
		FROM paid_clients
	`

	var rate float64
	err := s.db.GetContext(ctx, &rate, query)
	if err != nil {
		return 0, fmt.Errorf("db.GetContext: %w", err)
	}

	return rate, nil
}

// GetRevenueByTariff returns revenue breakdown by tariff for the given period
func (s *storageImpl) GetRevenueByTariff(ctx context.Context, start, end time.Time) ([]TariffRevenue, error) {
	query := `
		SELECT
			t.name as tariff_name,
			COALESCE(SUM(p.amount), 0) as revenue,
			COUNT(DISTINCT s.id) as count
		FROM tariffs t
		LEFT JOIN subscriptions s ON t.id = s.tariff_id
		LEFT JOIN payment_subscriptions ps ON s.id = ps.subscription_id
		LEFT JOIN payments p ON ps.payment_id = p.id
			AND p.status = 'approved'
			AND p.created_at >= ? AND p.created_at < ?
		WHERE t.is_active = 1
		GROUP BY t.id, t.name
		ORDER BY revenue DESC
	`

	rows, err := s.db.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("db.QueryContext: %w", err)
	}
	defer rows.Close()

	var result []TariffRevenue
	for rows.Next() {
		var tr TariffRevenue
		if err := rows.Scan(&tr.TariffName, &tr.Revenue, &tr.Count); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, tr)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return result, nil
}
