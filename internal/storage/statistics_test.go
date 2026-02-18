//go:build all || dbtest

package storage

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *storageImpl {
	t.Helper()

	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create minimal schema for trial conversion tests
	schema := `
		CREATE TABLE tariffs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			duration_days INTEGER NOT NULL,
			price DECIMAL(10,2) NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			tariff_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			client_whatsapp TEXT,
			generated_user_id TEXT,
			created_by_telegram_id INTEGER,
			activated_at TIMESTAMP,
			expires_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_renewed_at TIMESTAMP,
			renewal_count INTEGER DEFAULT 0,
			referrer_whatsapp TEXT,
			referral_type TEXT,
			server_id INTEGER,
			FOREIGN KEY (tariff_id) REFERENCES tariffs(id)
		);

		CREATE TABLE payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			amount DECIMAL(10,2) NOT NULL,
			status TEXT NOT NULL,
			yookassa_id TEXT UNIQUE,
			payment_url TEXT,
			processed_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE payment_subscriptions (
			payment_id INTEGER NOT NULL,
			subscription_id INTEGER NOT NULL,
			PRIMARY KEY (payment_id, subscription_id),
			FOREIGN KEY (payment_id) REFERENCES payments(id),
			FOREIGN KEY (subscription_id) REFERENCES subscriptions(id)
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return New(db)
}

// insertTariff inserts a tariff and returns its ID.
func insertTariff(t *testing.T, db *sqlx.DB, name string, price float64, durationDays int) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO tariffs (name, duration_days, price, is_active) VALUES (?, ?, ?, 1)",
		name, durationDays, price,
	)
	if err != nil {
		t.Fatalf("insert tariff: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// insertSubscription inserts a subscription and returns its ID.
func insertSubscription(t *testing.T, db *sqlx.DB, userID int64, tariffID int64, whatsapp string, renewalCount int) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO subscriptions (user_id, tariff_id, status, client_whatsapp, renewal_count) VALUES (?, ?, 'active', ?, ?)",
		userID, tariffID, whatsapp, renewalCount,
	)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// insertPayment inserts a payment and returns its ID.
func insertPayment(t *testing.T, db *sqlx.DB, userID int64, amount float64, status string) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO payments (user_id, amount, status) VALUES (?, ?, ?)",
		userID, amount, status,
	)
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// linkPayment links a payment to a subscription.
func linkPayment(t *testing.T, db *sqlx.DB, paymentID, subscriptionID int64) {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO payment_subscriptions (payment_id, subscription_id) VALUES (?, ?)",
		paymentID, subscriptionID,
	); err != nil {
		t.Fatalf("link payment: %v", err)
	}
}

func TestGetTrialConversionRate_NoData(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 0 {
		t.Errorf("expected 0%%, got %.1f%%", rate)
	}
}

func TestGetTrialConversionRate_TrialOnlyNoConversion(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	trialTariff := insertTariff(t, s.db, "Trial", 0, 7)

	// Two clients took trial, nobody converted
	insertSubscription(t, s.db, 1, trialTariff, "79001111111", 0)
	insertSubscription(t, s.db, 2, trialTariff, "79002222222", 0)

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 0 {
		t.Errorf("expected 0%%, got %.1f%%", rate)
	}
}

func TestGetTrialConversionRate_ConversionViaNewSubscription(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	trialTariff := insertTariff(t, s.db, "Trial", 0, 7)
	paidTariff := insertTariff(t, s.db, "Monthly", 300, 30)

	// Client A: trial only
	insertSubscription(t, s.db, 1, trialTariff, "79001111111", 0)

	// Client B: trial + paid (via new subscription path)
	insertSubscription(t, s.db, 2, trialTariff, "79002222222", 0)
	paidSub := insertSubscription(t, s.db, 2, paidTariff, "79002222222", 0)
	payment := insertPayment(t, s.db, 2, 300, "approved")
	linkPayment(t, s.db, payment, paidSub)

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 out of 2 trial clients converted = 50%
	if rate != 50 {
		t.Errorf("expected 50%%, got %.1f%%", rate)
	}
}

func TestGetTrialConversionRate_ConversionViaRenewal(t *testing.T) {
	// This is the main bug scenario: client's trial subscription has its
	// tariff_id changed to a paid tariff during renewal, and no
	// payment_subscriptions entry is created.
	s := setupTestDB(t)
	ctx := context.Background()

	trialTariff := insertTariff(t, s.db, "Trial", 0, 7)
	paidTariff := insertTariff(t, s.db, "Monthly", 300, 30)

	// Client A: trial only
	insertSubscription(t, s.db, 1, trialTariff, "79001111111", 0)

	// Client B: started trial, then renewed → tariff_id changed to paid.
	// No payment_subscriptions entry (renewal flow doesn't create one).
	insertSubscription(t, s.db, 2, paidTariff, "79002222222", 1) // tariff already changed, renewal_count > 0

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 out of 2 trial clients converted = 50%
	if rate != 50 {
		t.Errorf("expected 50%%, got %.1f%%", rate)
	}
}

func TestGetTrialConversionRate_AllConverted(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	trialTariff := insertTariff(t, s.db, "Trial", 0, 7)
	paidTariff := insertTariff(t, s.db, "Monthly", 300, 30)

	// Client A: conversion via new subscription
	insertSubscription(t, s.db, 1, trialTariff, "79001111111", 0)
	paidSub := insertSubscription(t, s.db, 1, paidTariff, "79001111111", 0)
	pay := insertPayment(t, s.db, 1, 300, "approved")
	linkPayment(t, s.db, pay, paidSub)

	// Client B: conversion via renewal (tariff changed)
	insertSubscription(t, s.db, 2, paidTariff, "79002222222", 1)

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate != 100 {
		t.Errorf("expected 100%%, got %.1f%%", rate)
	}
}

func TestGetTrialConversionRate_PaidOnlyUsersNotCounted(t *testing.T) {
	// Clients who never had a trial (first subscription was paid)
	// should not appear in the conversion calculation.
	s := setupTestDB(t)
	ctx := context.Background()

	trialTariff := insertTariff(t, s.db, "Trial", 0, 7)
	paidTariff := insertTariff(t, s.db, "Monthly", 300, 30)

	// Client A: trial, not converted
	insertSubscription(t, s.db, 1, trialTariff, "79001111111", 0)

	// Client B: paid only (no trial) — should NOT affect conversion rate
	paidSub := insertSubscription(t, s.db, 2, paidTariff, "79003333333", 0)
	pay := insertPayment(t, s.db, 2, 300, "approved")
	linkPayment(t, s.db, pay, paidSub)

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only 1 trial client (Client A), not converted → 0%
	if rate != 0 {
		t.Errorf("expected 0%%, got %.1f%%", rate)
	}
}

func TestGetTrialConversionRate_ReferralBonusNotCountedAsConversion(t *testing.T) {
	// Trial client whose renewal_count was bumped by referral bonus
	// (tariff still trial) should NOT be counted as converted.
	s := setupTestDB(t)
	ctx := context.Background()

	trialTariff := insertTariff(t, s.db, "Trial", 0, 7)

	// Client A: trial with referral bonus (renewal_count=1 but tariff still trial)
	insertSubscription(t, s.db, 1, trialTariff, "79001111111", 1)

	rate, err := s.GetTrialConversionRate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 trial client, not converted (just referral bonus) → 0%
	if rate != 0 {
		t.Errorf("expected 0%%, got %.1f%%", rate)
	}
}
