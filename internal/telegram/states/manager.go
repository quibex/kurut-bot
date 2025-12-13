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

// GetData получает данные пользователя
func (m *Manager) GetData(chatID int64) any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.userData[chatID]
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

// GetRenewSubData получает данные флоу продления подписки
func (m *Manager) GetRenewSubData(chatID int64) (*flows.RenewSubFlowData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	if !exists {
		return nil, fmt.Errorf("no data for chat %d", chatID)
	}

	flowData, ok := data.(*flows.RenewSubFlowData)
	if !ok {
		return nil, fmt.Errorf("invalid data type for chat %d", chatID)
	}

	return flowData, nil
}

// GetCreateSubForClientData получает данные флоу создания подписки для клиента
func (m *Manager) GetCreateSubForClientData(chatID int64) (*flows.CreateSubForClientFlowData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	if !exists {
		return nil, fmt.Errorf("no data for chat %d", chatID)
	}

	flowData, ok := data.(*flows.CreateSubForClientFlowData)
	if !ok {
		return nil, fmt.Errorf("invalid data type for chat %d", chatID)
	}

	return flowData, nil
}

// GetWelcomeData получает данные стартового флоу
func (m *Manager) GetWelcomeData(chatID int64) (*flows.WelcomeFlowData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	if !exists {
		return nil, fmt.Errorf("no data for chat %d", chatID)
	}

	flowData, ok := data.(*flows.WelcomeFlowData)
	if !ok {
		return nil, fmt.Errorf("invalid data type for chat %d", chatID)
	}

	return flowData, nil
}

// GetAddServerData получает данные флоу добавления сервера
func (m *Manager) GetAddServerData(chatID int64) (*flows.AddServerFlowData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.userData[chatID]
	if !exists {
		return nil, fmt.Errorf("no data for chat %d", chatID)
	}

	flowData, ok := data.(*flows.AddServerFlowData)
	if !ok {
		return nil, fmt.Errorf("invalid data type for chat %d", chatID)
	}

	return flowData, nil
}
