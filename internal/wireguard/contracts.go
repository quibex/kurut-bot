package wireguard

import (
	"context"

	"kurut-bot/internal/storage"

	pb "github.com/quibex/wg-agent/pkg/api/proto"
)

type WGClient interface {
	CreateClient(ctx context.Context, userID string) (*pb.CreateClientResponse, error)
	DisableClient(ctx context.Context, userID string) error
	EnableClient(ctx context.Context, userID string) error
	DeleteClient(ctx context.Context, userID string) error
	GetClient(ctx context.Context, userID string) (*pb.GetClientResponse, error)
	ListClients(ctx context.Context) (*pb.ListClientsResponse, error)
	Close() error
}

type Storage interface {
	IncrementWGServerPeerCount(ctx context.Context, serverID int64) error
	DecrementWGServerPeerCount(ctx context.Context, serverID int64) error
	GetWGServer(ctx context.Context, id int64) (*storage.WGServer, error)
}

type Balancer interface {
	SelectServer(ctx context.Context) (*storage.WGServer, error)
}

type TLSConfig interface {
	GetCACertPath() string
	GetClientCertPath() string
	GetClientKeyPath() string
	GetServerName() string
}

type ClientConfig struct {
	ServerID     int64
	UserID       string
	ConfigFile   string
	QRCodeBase64 string
	DeepLink     string
	ClientIP     string
}
