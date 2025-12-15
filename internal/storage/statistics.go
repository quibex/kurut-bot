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
	PreviousMonthRevenue     float64
	CurrentMonthRevenue      float64
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
		Select("t.id as tariff_id", "t.name as tariff_name", "COUNT(DISTINCT s.user_id) as user_count").
		From(tariffsTable+" t").
		LeftJoin(subscriptionsTable+" s ON t.id = s.tariff_id AND s.status = 'active'").
		Where(sq.Eq{"t.is_active": true}).
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

func (s *storageImpl) GetArchivedTariffStatistics(ctx context.Context) ([]TariffStats, error) {
	query := s.stmpBuilder().
		Select("t.id as tariff_id", "t.name as tariff_name", "COUNT(DISTINCT s.user_id) as user_count").
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

	previousMonthRevenue, err := s.GetRevenueForMonth(ctx, previousYear, previousMonth)
	if err != nil {
		return nil, fmt.Errorf("get previous month revenue: %w", err)
	}

	currentMonthRevenue, err := s.GetRevenueForMonth(ctx, currentYear, currentMonth)
	if err != nil {
		return nil, fmt.Errorf("get current month revenue: %w", err)
	}

	return &StatisticsData{
		ActiveSubscriptionsCount: activeSubsCount,
		ActiveUsersCount:         activeUsersCount,
		InactiveUsersCount:       inactiveUsersCount,
		ActiveTariffStats:        activeTariffStats,
		ArchivedTariffStats:      archivedTariffStats,
		PreviousMonthRevenue:     previousMonthRevenue,
		CurrentMonthRevenue:      currentMonthRevenue,
	}, nil
}
