package assistants

import "time"

// Assistant is a staff member with the assistant role, managed from the admin
// panel. Assistants get the client-facing tooling but never money or stats.
// The legacy TELEGRAM_ASSISTANT_IDS env var still grants the role too — the
// AdminChecker unions both sources.
type Assistant struct {
	ID         int64
	TelegramID int64
	Label      string
	AddedBy    int64
	CreatedAt  time.Time
}

// GetCriteria selects a single assistant.
type GetCriteria struct {
	TelegramID *int64
}

// DeleteCriteria selects assistants to remove.
type DeleteCriteria struct {
	TelegramID *int64
}
