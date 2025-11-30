package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"kurut-bot/internal/infra/wireguard"
	"kurut-bot/internal/storage"
	"kurut-bot/internal/stories/subs"
)

type Service struct {
	storage   Storage
	balancer  Balancer
	logger    *slog.Logger
	tlsConfig TLSConfig
	clients   map[int64]WGClient
	mu        sync.RWMutex
}

func NewService(storage Storage, balancer Balancer, tlsConfig TLSConfig, logger *slog.Logger) *Service {
	return &Service{
		storage:   storage,
		balancer:  balancer,
		tlsConfig: tlsConfig,
		logger:    logger,
		clients:   make(map[int64]WGClient),
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
		caCertPath := s.tlsConfig.GetCACertPath()
		clientCertPath := s.tlsConfig.GetClientCertPath()
		clientKeyPath := s.tlsConfig.GetClientKeyPath()
		serverName := s.tlsConfig.GetServerName()

		if server.TLSServerName != nil && *server.TLSServerName != "" {
			serverName = *server.TLSServerName
		}

		newClient, err = wireguard.NewClientWithMTLS(
			server.GRPCAddress,
			true,
			caCertPath,
			clientCertPath,
			clientKeyPath,
			serverName,
			s.logger,
		)
	} else {
		newClient, err = wireguard.NewClient(server.GRPCAddress, s.logger)
	}

	if err != nil {
		return nil, fmt.Errorf("create wg client: %w", err)
	}

	s.clients[server.ID] = newClient
	return newClient, nil
}

func (s *Service) CreateClient(ctx context.Context, userID string) (*ClientConfig, error) {
	server, err := s.balancer.SelectServer(ctx)
	if err != nil {
		return nil, fmt.Errorf("select server: %w", err)
	}

	client, err := s.getOrCreateClient(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}

	resp, err := client.CreateClient(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	if err := s.storage.IncrementWGServerPeerCount(ctx, server.ID); err != nil {
		s.logger.Error("Failed to increment peer count", "server_id", server.ID, "error", err)
	}

	return &ClientConfig{
		ServerID:     server.ID,
		UserID:       userID,
		ConfigFile:   resp.ConfigFile,
		QRCodeBase64: resp.QrCodeBase64,
		DeepLink:     resp.DeepLink,
		ClientIP:     resp.ClientIp,
	}, nil
}

func (s *Service) DisableClient(ctx context.Context, subscription *subs.Subscription) error {
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

	if err := client.DisableClient(ctx, wgData.UserID); err != nil {
		return fmt.Errorf("disable client: %w", err)
	}

	return nil
}

func (s *Service) EnableClient(ctx context.Context, subscription *subs.Subscription) error {
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

	if err := client.EnableClient(ctx, wgData.UserID); err != nil {
		return fmt.Errorf("enable client: %w", err)
	}

	return nil
}

func (s *Service) DeleteClient(ctx context.Context, subscription *subs.Subscription) error {
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

	if err := client.DeleteClient(ctx, wgData.UserID); err != nil {
		return fmt.Errorf("delete client: %w", err)
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
