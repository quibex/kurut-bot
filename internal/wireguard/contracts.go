package wireguard

import (
	"context"

	"kurut-bot/internal/storage"

	pb "github.com/quibex/wg-agent/pkg/api/proto"
)

type WGClient interface {
	GeneratePeerConfig(ctx context.Context, req *pb.GeneratePeerConfigRequest) (*pb.GeneratePeerConfigResponse, error)
	AddPeer(ctx context.Context, req *pb.AddPeerRequest) (*pb.AddPeerResponse, error)
	RemovePeer(ctx context.Context, req *pb.RemovePeerRequest) error
	DisablePeer(ctx context.Context, req *pb.DisablePeerRequest) error
	EnablePeer(ctx context.Context, req *pb.EnablePeerRequest) error
	GetPeerInfo(ctx context.Context, req *pb.GetPeerInfoRequest) (*pb.GetPeerInfoResponse, error)
	ListPeers(ctx context.Context, req *pb.ListPeersRequest) (*pb.ListPeersResponse, error)
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

type PeerConfig struct {
	ServerID   int64
	PublicKey  string
	PrivateKey string
	AllowedIP  string
	Config     string
	QRCode     string
}

