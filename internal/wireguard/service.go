package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"kurut-bot/internal/infra/wireguard"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/subs"

	pb "github.com/quibex/wg-agent/pkg/api/proto"
)

type Service struct {
	storage  Storage
	balancer Balancer
	logger   *slog.Logger
	clients  map[int64]WGClient
	mu       sync.RWMutex
}

func NewService(storage Storage, balancer Balancer, logger *slog.Logger) *Service {
	return &Service{
		storage:  storage,
		balancer: balancer,
		logger:   logger,
		clients:  make(map[int64]WGClient),
	}
}

func (s *Service) getOrCreateClient(ctx context.Context, server *storage.WGServer) (WGClient, error) {
	s.mu.RLock()
	client, exists := s.clients[server.ID]
	s.mu.RUnlock()

	if exists {
		return client, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	client, exists = s.clients[server.ID]
	if exists {
		return client, nil
	}

	var newClient *wireguard.Client
	var err error

	if server.TLSEnabled {
		certPath := ""
		serverName := ""
		if server.TLSCertPath != nil {
			certPath = *server.TLSCertPath
		}
		if server.TLSServerName != nil {
			serverName = *server.TLSServerName
		}
		newClient, err = wireguard.NewClientWithTLS(server.GRPCAddress, true, certPath, serverName, s.logger)
	} else {
		newClient, err = wireguard.NewClient(server.GRPCAddress, s.logger)
	}

	if err != nil {
		return nil, fmt.Errorf("create wg client: %w", err)
	}

	s.clients[server.ID] = newClient
	return newClient, nil
}

func (s *Service) CreatePeer(ctx context.Context, userID int64, peerID string) (*PeerConfig, error) {
	server, err := s.balancer.SelectServer(ctx)
	if err != nil {
		return nil, fmt.Errorf("select server: %w", err)
	}

	client, err := s.getOrCreateClient(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}

	genResp, err := client.GeneratePeerConfig(ctx, &pb.GeneratePeerConfigRequest{
		Interface:      server.Interface,
		ServerEndpoint: server.Endpoint,
		DnsServers:     server.DNSServers,
		AllowedIps:     "0.0.0.0/0",
	})
	if err != nil {
		return nil, fmt.Errorf("generate peer config: %w", err)
	}

	addResp, err := client.AddPeer(ctx, &pb.AddPeerRequest{
		Interface:  server.Interface,
		PublicKey:  genResp.PublicKey,
		AllowedIp:  genResp.AllowedIp,
		KeepaliveS: 25,
		PeerId:     peerID,
	})
	if err != nil {
		return nil, fmt.Errorf("add peer: %w", err)
	}

	if err := s.storage.IncrementWGServerPeerCount(ctx, server.ID); err != nil {
		s.logger.Error("Failed to increment peer count", "server_id", server.ID, "error", err)
	}

	return &PeerConfig{
		ServerID:   server.ID,
		PublicKey:  genResp.PublicKey,
		PrivateKey: genResp.PrivateKey,
		AllowedIP:  genResp.AllowedIp,
		Config:     addResp.Config,
		QRCode:     addResp.QrCode,
	}, nil
}

func (s *Service) DisablePeer(ctx context.Context, subscription *subs.Subscription) error {
	wgData, err := subscription.GetWireGuardData()
	if err != nil {
		return fmt.Errorf("get wireguard data: %w", err)
	}
	if wgData == nil {
		return fmt.Errorf("subscription has no wireguard data")
	}

	server, err := s.storage.GetWGServer(ctx, wgData.ServerID)
	if err != nil {
		return fmt.Errorf("get server: %w", err)
	}
	if server == nil {
		return fmt.Errorf("server not found: %d", wgData.ServerID)
	}

	client, err := s.getOrCreateClient(ctx, server)
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}

	if err := client.DisablePeer(ctx, &pb.DisablePeerRequest{
		Interface: server.Interface,
		PublicKey: wgData.PublicKey,
	}); err != nil {
		return fmt.Errorf("disable peer: %w", err)
	}

	return nil
}

func (s *Service) EnablePeer(ctx context.Context, subscription *subs.Subscription) error {
	wgData, err := subscription.GetWireGuardData()
	if err != nil {
		return fmt.Errorf("get wireguard data: %w", err)
	}
	if wgData == nil {
		return fmt.Errorf("subscription has no wireguard data")
	}

	server, err := s.storage.GetWGServer(ctx, wgData.ServerID)
	if err != nil {
		return fmt.Errorf("get server: %w", err)
	}
	if server == nil {
		return fmt.Errorf("server not found: %d", wgData.ServerID)
	}

	client, err := s.getOrCreateClient(ctx, server)
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}

	if err := client.EnablePeer(ctx, &pb.EnablePeerRequest{
		Interface: server.Interface,
		PublicKey: wgData.PublicKey,
	}); err != nil {
		return fmt.Errorf("enable peer: %w", err)
	}

	return nil
}

func (s *Service) RemovePeer(ctx context.Context, subscription *subs.Subscription) error {
	wgData, err := subscription.GetWireGuardData()
	if err != nil {
		return fmt.Errorf("get wireguard data: %w", err)
	}
	if wgData == nil {
		return fmt.Errorf("subscription has no wireguard data")
	}

	server, err := s.storage.GetWGServer(ctx, wgData.ServerID)
	if err != nil {
		return fmt.Errorf("get server: %w", err)
	}
	if server == nil {
		return fmt.Errorf("server not found: %d", wgData.ServerID)
	}

	client, err := s.getOrCreateClient(ctx, server)
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}

	if err := client.RemovePeer(ctx, &pb.RemovePeerRequest{
		Interface: server.Interface,
		PublicKey: wgData.PublicKey,
	}); err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}

	if err := s.storage.DecrementWGServerPeerCount(ctx, wgData.ServerID); err != nil {
		s.logger.Error("Failed to decrement peer count", "server_id", wgData.ServerID, "error", err)
	}

	return nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, client := range s.clients {
		if err := client.Close(); err != nil {
			s.logger.Error("Failed to close client", "server_id", id, "error", err)
		}
	}

	s.clients = make(map[int64]WGClient)
	return nil
}
