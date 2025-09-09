package telegram

import (
	"kurut-bot/internal/config"
	"slices"
)

// AdminChecker проверяет является ли пользователь админом
type AdminChecker struct {
	adminIDs []int64
}

// NewAdminChecker создает новый проверялка админов
func NewAdminChecker(cfg *config.TelegramConfig) *AdminChecker {
	return &AdminChecker{
		adminIDs: cfg.AdminTelegramIDs,
	}
}

// IsAdmin проверяет является ли пользователь с данным Telegram ID админом
func (a *AdminChecker) IsAdmin(telegramID int64) bool {
	return slices.Contains(a.adminIDs, telegramID)
}
