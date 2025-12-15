package telegram

import (
	"kurut-bot/internal/config"
	"slices"
)

// AdminChecker проверяет является ли пользователь админом или ассистентом
type AdminChecker struct {
	adminIDs     []int64
	assistantIDs []int64
}

// NewAdminChecker создает новый проверялка админов
func NewAdminChecker(cfg *config.TelegramConfig) *AdminChecker {
	return &AdminChecker{
		adminIDs:     cfg.AdminIDs,
		assistantIDs: cfg.AssistantIDs,
	}
}

// IsAdmin проверяет является ли пользователь с данным Telegram ID админом
func (a *AdminChecker) IsAdmin(telegramID int64) bool {
	return slices.Contains(a.adminIDs, telegramID)
}

// IsAssistant проверяет является ли пользователь ассистентом
func (a *AdminChecker) IsAssistant(telegramID int64) bool {
	return slices.Contains(a.assistantIDs, telegramID)
}

// IsAllowedUser проверяет имеет ли пользователь доступ к боту (админ или ассистент)
func (a *AdminChecker) IsAllowedUser(telegramID int64) bool {
	return a.IsAdmin(telegramID) || a.IsAssistant(telegramID)
}
