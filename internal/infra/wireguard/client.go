package wireguard

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/quibex/wg-agent/pkg/api/proto"
)

type Client struct {
	address string
	conn    *grpc.ClientConn
	client  pb.WireGuardAgentClient
	logger  *slog.Logger
}

func NewClient(address string, logger *slog.Logger) (*Client, error) {
	return NewClientWithTLS(address, false, "", "", logger)
}

func NewClientWithTLS(address string, tlsEnabled bool, caCertPath string, serverName string, logger *slog.Logger) (*Client, error) {
	return NewClientWithMTLS(address, tlsEnabled, caCertPath, "", "", serverName, logger)
}

func NewClientWithMTLS(address string, tlsEnabled bool, caCertPath, clientCertPath, clientKeyPath string, serverName string, logger *slog.Logger) (*Client, error) {
	var opts []grpc.DialOption

	if tlsEnabled && caCertPath != "" {
		if clientCertPath != "" && clientKeyPath != "" {
			// Load client's certificate and private key
			clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load client cert/key: %w", err)
			}

			// Load CA cert
			caCert, err := os.ReadFile(caCertPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA cert: %w", err)
			}

			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA cert")
			}

			// Create TLS config with mTLS
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      caCertPool,
				ServerName:   serverName,
			}

			creds := credentials.NewTLS(tlsConfig)
			opts = append(opts, grpc.WithTransportCredentials(creds))
			logger.Info("Using mTLS for gRPC connection",
				"ca_cert", caCertPath,
				"client_cert", clientCertPath,
				"server_name", serverName)
		} else {
			creds, err := credentials.NewClientTLSFromFile(caCertPath, serverName)
			if err != nil {
				return nil, fmt.Errorf("failed to load TLS credentials from %s: %w", caCertPath, err)
			}
			opts = append(opts, grpc.WithTransportCredentials(creds))
			logger.Info("Using TLS for gRPC connection", "ca_cert", caCertPath, "server_name", serverName)
		}
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if tlsEnabled {
			logger.Warn("TLS enabled but no cert path provided, falling back to insecure connection")
		}
	}

	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to wg-agent at %s: %w", address, err)
	}

	return &Client{
		address: address,
		conn:    conn,
		client:  pb.NewWireGuardAgentClient(conn),
		logger:  logger,
	}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) CreateClient(ctx context.Context, userID string) (*pb.CreateClientResponse, error) {
	c.logger.Debug("Creating client", "user_id", userID)

	resp, err := c.client.CreateClient(ctx, &pb.CreateClientRequest{
		UserId: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	return resp, nil
}

func (c *Client) DisableClient(ctx context.Context, userID string) error {
	c.logger.Debug("Disabling client", "user_id", userID)

	_, err := c.client.DisableClient(ctx, &pb.DisableClientRequest{
		UserId: userID,
	})
	if err != nil {
		return fmt.Errorf("disable client: %w", err)
	}

	return nil
}

func (c *Client) EnableClient(ctx context.Context, userID string) error {
	c.logger.Debug("Enabling client", "user_id", userID)

	_, err := c.client.EnableClient(ctx, &pb.EnableClientRequest{
		UserId: userID,
	})
	if err != nil {
		return fmt.Errorf("enable client: %w", err)
	}

	return nil
}

func (c *Client) DeleteClient(ctx context.Context, userID string) error {
	c.logger.Debug("Deleting client", "user_id", userID)

	_, err := c.client.DeleteClient(ctx, &pb.DeleteClientRequest{
		UserId: userID,
	})
	if err != nil {
		return fmt.Errorf("delete client: %w", err)
	}

	return nil
}

func (c *Client) GetClient(ctx context.Context, userID string) (*pb.GetClientResponse, error) {
	c.logger.Debug("Getting client", "user_id", userID)

	resp, err := c.client.GetClient(ctx, &pb.GetClientRequest{
		UserId: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}

	return resp, nil
}

func (c *Client) ListClients(ctx context.Context) (*pb.ListClientsResponse, error) {
	c.logger.Debug("Listing clients")

	resp, err := c.client.ListClients(ctx, &pb.ListClientsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}

	return resp, nil
}
