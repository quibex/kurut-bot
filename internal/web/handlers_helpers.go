package web

import (
	"fmt"
	"time"

	"kurut-bot/internal/stories/subs"
)

var moscowLocation = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}
	return loc
}()

func formatRussianDate(t time.Time) string {
	months := []string{
		"", "января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	tMoscow := t.In(moscowLocation)
	return fmt.Sprintf("%d %s %d", tMoscow.Day(), months[tMoscow.Month()], tMoscow.Year())
}

func isToday(t time.Time) bool {
	now := time.Now().In(moscowLocation)
	tMoscow := t.In(moscowLocation)
	return now.Year() == tMoscow.Year() && now.YearDay() == tMoscow.YearDay()
}

func formatRemainingTime(t time.Time) string {
	nowMoscow := time.Now().In(moscowLocation)
	tMoscow := t.In(moscowLocation)

	todayDate := time.Date(nowMoscow.Year(), nowMoscow.Month(), nowMoscow.Day(), 0, 0, 0, 0, moscowLocation)
	expiryDate := time.Date(tMoscow.Year(), tMoscow.Month(), tMoscow.Day(), 0, 0, 0, 0, moscowLocation)

	if expiryDate.Equal(todayDate) {
		return "истекает сегодня"
	}
	if expiryDate.Before(todayDate) {
		return "истекла"
	}

	days := int(expiryDate.Sub(todayDate).Hours() / 24)

	// Simple pluralization for "days"
	suffix := "дней"
	lastDigit := days % 10
	lastTwoDigits := days % 100

	if lastTwoDigits >= 11 && lastTwoDigits <= 19 {
		suffix = "дней"
	} else if lastDigit == 1 {
		suffix = "день"
	} else if lastDigit >= 2 && lastDigit <= 4 {
		suffix = "дня"
	}

	return fmt.Sprintf("осталось %d %s", days, suffix)
}

// daysRemaining возвращает число целых дней до истечения (0, если уже истекло).
func daysRemaining(t time.Time) int {
	nowMoscow := time.Now().In(moscowLocation)
	tMoscow := t.In(moscowLocation)

	todayDate := time.Date(nowMoscow.Year(), nowMoscow.Month(), nowMoscow.Day(), 0, 0, 0, 0, moscowLocation)
	expiryDate := time.Date(tMoscow.Year(), tMoscow.Month(), tMoscow.Day(), 0, 0, 0, 0, moscowLocation)

	if !expiryDate.After(todayDate) {
		return 0
	}
	return int(expiryDate.Sub(todayDate).Hours() / 24)
}

// maxRemainingDays возвращает максимальный остаток дней по подпискам клиента
// (0, если активных дней нет) — его и переносим на новый VPN.
func maxRemainingDays(subscriptions []*subs.Subscription) int {
	maxDays := 0
	for _, sub := range subscriptions {
		if sub.ExpiresAt == nil {
			continue
		}
		if d := daysRemaining(*sub.ExpiresAt); d > maxDays {
			maxDays = d
		}
	}
	return maxDays
}

// pluralizeDays склоняет слово «день» под число (1 день, 2 дня, 5 дней).
func pluralizeDays(days int) string {
	lastDigit := days % 10
	lastTwoDigits := days % 100

	switch {
	case lastTwoDigits >= 11 && lastTwoDigits <= 19:
		return "дней"
	case lastDigit == 1:
		return "день"
	case lastDigit >= 2 && lastDigit <= 4:
		return "дня"
	default:
		return "дней"
	}
}
