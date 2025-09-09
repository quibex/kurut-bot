package states

type StateManager interface {
	GetState(userID int64) State
	SetState(userID int64, state State, data any)
	GetStateData(userID int64) (any, bool)
	Clear(userID int64)
}