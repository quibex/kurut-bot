package telegram

import (
	"context"
	"testing"

	"kurut-bot/internal/config"
)

type fakeRosterStore struct {
	ids []int64
	err error
}

func (f *fakeRosterStore) ListAssistantTelegramIDs(_ context.Context) ([]int64, error) {
	return f.ids, f.err
}

func TestAdminChecker_EnvRolesAndUnknown(t *testing.T) {
	cfg := &config.TelegramConfig{AdminIDs: []int64{111}, AssistantIDs: []int64{222}}
	ac := NewAdminChecker(cfg, nil)

	if !ac.IsAdmin(111) {
		t.Error("111 should be admin")
	}
	if !ac.IsAssistant(222) {
		t.Error("222 should be env assistant")
	}
	if !ac.IsAllowedUser(111) || !ac.IsAllowedUser(222) {
		t.Error("admin and assistant should be allowed")
	}
	if ac.IsAssistant(333) || ac.IsAllowedUser(333) {
		t.Error("333 should not have access")
	}
}

func TestAdminChecker_ReloadFromStore(t *testing.T) {
	cfg := &config.TelegramConfig{AssistantIDs: []int64{222}}
	ac := NewAdminChecker(cfg, &fakeRosterStore{ids: []int64{444, 555}})

	if ac.IsAssistant(444) {
		t.Error("444 should not be an assistant before reload")
	}
	if err := ac.ReloadAssistants(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !ac.IsAssistant(444) || !ac.IsAssistant(555) {
		t.Error("444/555 should be assistants from DB after reload")
	}
	if !ac.IsAssistant(222) {
		t.Error("env assistant 222 should still pass after reload")
	}
}

func TestAdminChecker_AddRemove(t *testing.T) {
	ac := NewAdminChecker(&config.TelegramConfig{}, nil)

	if ac.IsAssistant(999) {
		t.Error("999 should not be assistant initially")
	}
	ac.AddAssistant(999)
	if !ac.IsAssistant(999) || !ac.IsAllowedUser(999) {
		t.Error("999 should be assistant after AddAssistant")
	}
	ac.RemoveAssistant(999)
	if ac.IsAssistant(999) {
		t.Error("999 should be removed after RemoveAssistant")
	}
}

func TestAdminChecker_ReloadNilStore(t *testing.T) {
	ac := NewAdminChecker(&config.TelegramConfig{}, nil)
	if err := ac.ReloadAssistants(context.Background()); err != nil {
		t.Errorf("reload with nil store should be no-op, got %v", err)
	}
}
