package servers

import "context"

type (
	Storage interface {
		CreateServer(ctx context.Context, server Server) (*Server, error)
		GetServer(ctx context.Context, criteria GetCriteria) (*Server, error)
		UpdateServer(ctx context.Context, criteria GetCriteria, params UpdateParams) (*Server, error)
		ListServers(ctx context.Context, criteria ListCriteria) ([]*Server, error)
		GetAvailableServer(ctx context.Context) (*Server, error)
		IncrementServerUsers(ctx context.Context, serverID int64) error
		DecrementServerUsers(ctx context.Context, serverID int64) error
	}
)
