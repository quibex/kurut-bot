package states

import (
	"fmt"
	"sync"

	"kurut-bot/internal/telegram/flows"
)

// Manager управляет состояниями пользователей в памяти
type Manager struct {
	mu         sync.RWMutex
	userStates map[int64]State
	userData   map[int64]any
}

// NewManager создает новый менеджер состояний
func NewManager() *Manager {
	return &Manager{
		userStates: make(map[int64]State),
		userData:   make(map[int64]any),
	}
}

// GetState получает текущее состояние пользователя
func (m *Manager) GetState(chatID int64) State {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.userStates[chatID]
	if !exists {
		return StateNone
	}
	return state
}

// SetState устанавливает состояние пользователя
func (m *Manager) SetState(chatID int64, state State, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userStates[chatID] = state
	if data != nil {
		m.userData[chatID] = data
	}
}

// GetStateData получает данные состояния пользователя
func (m *Manager) GetStateData(chatID int64) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	return data, exists
}

// Clear очищает состояние пользователя
func (m *Manager) Clear(chatID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.userStates, chatID)
	delete(m.userData, chatID)
}

// GetBuySubData получает данные флоу покупки подписки
func (m *Manager) GetBuySubData(chatID int64) (*flows.BuySubFlowData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	if !exists {
		return nil, fmt.Errorf("no data for chat %d", chatID)
	}

	flowData, ok := data.(*flows.BuySubFlowData)
	if !ok {
		return nil, fmt.Errorf("invalid data type for chat %d", chatID)
	}

	return flowData, nil
}

// SetBuySubState устанавливает состояние и данные флоу покупки
func (m *Manager) SetBuySubState(tgUserID int64, state State, data *flows.BuySubFlowData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userStates[tgUserID] = state
	m.userData[tgUserID] = data
	return nil
}

// GetCreateTariffData получает данные флоу создания тарифа
func (m *Manager) GetCreateTariffData(chatID int64) (*flows.CreateTariffFlowData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	if !exists {
		return nil, fmt.Errorf("no data for chat %d", chatID)
	}

	flowData, ok := data.(*flows.CreateTariffFlowData)
	if !ok {
		return nil, fmt.Errorf("invalid data type for chat %d", chatID)
	}

	return flowData, nil
}

// SetCreateTariffState устанавливает состояние и данные флоу создания тарифа
func (m *Manager) SetCreateTariffState(tgUserID int64, state State, data *flows.CreateTariffFlowData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userStates[tgUserID] = state
	m.userData[tgUserID] = data
	return nil
}
