package wireguard

import (
	"context"
	"fmt"
	"log/slog"

	"kurut-bot/internal/storage"
)

type Balancer struct {
	storage Storage
	logger  *slog.Logger
}

type Storage interface {
	ListEnabledWGServers(ctx context.Context) ([]*storage.WGServer, error)
}

func NewBalancer(storage Storage, logger *slog.Logger) *Balancer {
	return &Balancer{
		storage: storage,
		logger:  logger,
	}
}

func (b *Balancer) SelectServer(ctx context.Context) (*storage.WGServer, error) {
	servers, err := b.storage.ListEnabledWGServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled servers: %w", err)
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no enabled WireGuard servers available")
	}

	for _, server := range servers {
		if server.CurrentPeers < server.MaxPeers {
			b.logger.Debug("Selected server",
				"server_id", server.ID,
				"name", server.Name,
				"current_peers", server.CurrentPeers,
				"max_peers", server.MaxPeers)
			return server, nil
		}
	}

	return nil, fmt.Errorf("all WireGuard servers are at capacity")
}

