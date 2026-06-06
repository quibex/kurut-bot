package telegram

import (
	"context"
	"slices"
	"sync"

	"kurut-bot/internal/config"
)

// assistantRosterStore reads the panel-managed assistant roster from the DB.
type assistantRosterStore interface {
	ListAssistantTelegramIDs(ctx context.Context) ([]int64, error)
}

// AdminChecker проверяет является ли пользователь админом или ассистентом.
//
// Ассистенты складываются из двух источников: статичный env-список
// (TELEGRAM_ASSISTANT_IDS) и панель-управляемый ростер из БД, который кэшируется
// в памяти и обновляется на добавление/удаление через админ-панель.
type AdminChecker struct {
	adminIDs        []int64
	envAssistantIDs []int64

	store        assistantRosterStore
	mu           sync.RWMutex
	dbAssistants map[int64]struct{}
}

// NewAdminChecker создает новую проверялку. store может быть nil (тогда работает
// только env-список ассистентов).
func NewAdminChecker(cfg *config.TelegramConfig, store assistantRosterStore) *AdminChecker {
	return &AdminChecker{
		adminIDs:        cfg.AdminIDs,
		envAssistantIDs: cfg.AssistantIDs,
		store:           store,
		dbAssistants:    make(map[int64]struct{}),
	}
}

// ReloadAssistants перечитывает ростер из БД в кэш (вызывается на старте).
func (a *AdminChecker) ReloadAssistants(ctx context.Context) error {
	if a.store == nil {
		return nil
	}
	ids, err := a.store.ListAssistantTelegramIDs(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dbAssistants = make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		a.dbAssistants[id] = struct{}{}
	}
	return nil
}

// AddAssistant добавляет id в кэш (после записи в БD из панели).
func (a *AdminChecker) AddAssistant(telegramID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dbAssistants[telegramID] = struct{}{}
}

// RemoveAssistant убирает id из кэша (после удаления из БД).
func (a *AdminChecker) RemoveAssistant(telegramID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.dbAssistants, telegramID)
}

// IsAdmin проверяет является ли пользователь с данным Telegram ID админом
func (a *AdminChecker) IsAdmin(telegramID int64) bool {
	return slices.Contains(a.adminIDs, telegramID)
}

// IsAssistant проверяет является ли пользователь ассистентом (env ∪ кэш БД)
func (a *AdminChecker) IsAssistant(telegramID int64) bool {
	if slices.Contains(a.envAssistantIDs, telegramID) {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.dbAssistants[telegramID]
	return ok
}

// IsAllowedUser проверяет имеет ли пользователь доступ к боту (админ или ассистент)
func (a *AdminChecker) IsAllowedUser(telegramID int64) bool {
	return a.IsAdmin(telegramID) || a.IsAssistant(telegramID)
}
