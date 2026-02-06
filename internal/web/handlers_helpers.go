package web

import (
	"fmt"
	"time"
)

func formatRussianDate(t time.Time) string {
	months := []string{
		"", "января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	month := t.Month()
	return fmt.Sprintf("%d %s %d", t.Day(), months[month], t.Year())
}

func formatRemainingTime(t time.Time) string {
	now := time.Now()
	if t.Before(now) {
		return "истекла"
	}

	diff := t.Sub(now)
	days := int(diff.Hours() / 24)

	if days == 0 {
		return "сегодня"
	}

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
