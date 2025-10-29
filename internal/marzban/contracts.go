package marzban

import (
	"context"

	marzbanAPI "kurut-bot/pkg/marzban"
)

type Client interface {
	AddUser(ctx context.Context, request *marzbanAPI.UserCreate) (marzbanAPI.AddUserRes, error)
	GetInbounds(ctx context.Context) (marzbanAPI.GetInboundsRes, error)
	ModifyUser(ctx context.Context, request *marzbanAPI.UserModify, params marzbanAPI.ModifyUserParams) (marzbanAPI.ModifyUserRes, error)
}
