package users

import "context"

type (
	Storage interface {
		CreateUser(ctx context.Context, user User) (*User, error)
		GetUser(ctx context.Context, criteria GetCriteria) (*User, error)
		UpdateUser(ctx context.Context, criteria GetCriteria, params UpdateParams) (*User, error)
	}
)
