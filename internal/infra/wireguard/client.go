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

func (c *Client) GeneratePeerConfig(ctx context.Context, req *pb.GeneratePeerConfigRequest) (*pb.GeneratePeerConfigResponse, error) {
	c.logger.Debug("Generating peer config", "interface", req.Interface)

	resp, err := c.client.GeneratePeerConfig(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("generate peer config: %w", err)
	}

	return resp, nil
}

func (c *Client) AddPeer(ctx context.Context, req *pb.AddPeerRequest) (*pb.AddPeerResponse, error) {
	c.logger.Debug("Adding peer", "interface", req.Interface, "peer_id", req.PeerId)

	resp, err := c.client.AddPeer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("add peer: %w", err)
	}

	return resp, nil
}

func (c *Client) RemovePeer(ctx context.Context, req *pb.RemovePeerRequest) error {
	c.logger.Debug("Removing peer", "interface", req.Interface, "public_key", req.PublicKey)

	_, err := c.client.RemovePeer(ctx, req)
	if err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}

	return nil
}

func (c *Client) DisablePeer(ctx context.Context, req *pb.DisablePeerRequest) error {
	c.logger.Debug("Disabling peer", "interface", req.Interface, "public_key", req.PublicKey)

	_, err := c.client.DisablePeer(ctx, req)
	if err != nil {
		return fmt.Errorf("disable peer: %w", err)
	}

	return nil
}

func (c *Client) EnablePeer(ctx context.Context, req *pb.EnablePeerRequest) error {
	c.logger.Debug("Enabling peer", "interface", req.Interface, "public_key", req.PublicKey)

	_, err := c.client.EnablePeer(ctx, req)
	if err != nil {
		return fmt.Errorf("enable peer: %w", err)
	}

	return nil
}

func (c *Client) GetPeerInfo(ctx context.Context, req *pb.GetPeerInfoRequest) (*pb.GetPeerInfoResponse, error) {
	c.logger.Debug("Getting peer info", "interface", req.Interface, "public_key", req.PublicKey)

	resp, err := c.client.GetPeerInfo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get peer info: %w", err)
	}

	return resp, nil
}

func (c *Client) ListPeers(ctx context.Context, req *pb.ListPeersRequest) (*pb.ListPeersResponse, error) {
	c.logger.Debug("Listing peers", "interface", req.Interface)

	resp, err := c.client.ListPeers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}

	return resp, nil
}

