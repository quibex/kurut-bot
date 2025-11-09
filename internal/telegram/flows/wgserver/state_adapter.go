package wgserver

import (
	"kurut-bot/internal/telegram/states"
)

type stateManagerAdapter struct {
	manager *states.Manager
}

func NewStateManagerAdapter(manager *states.Manager) StateManager {
	return &stateManagerAdapter{manager: manager}
}

func (a *stateManagerAdapter) GetState(chatID int64) (string, interface{}) {
	state := a.manager.GetState(chatID)
	data := a.manager.GetData(chatID)
	return string(state), data
}

func (a *stateManagerAdapter) SetState(chatID int64, state string, data interface{}) {
	a.manager.SetState(chatID, states.State(state), data)
}

