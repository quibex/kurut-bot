package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

func (w *WireGuardConfig) PrepareCertFiles() error {
	if w.TLSCACert == "" || w.TLSClientCert == "" || w.TLSClientKey == "" {
		return fmt.Errorf("TLS certificates are required (CA, client cert, client key)")
	}

	if err := os.MkdirAll(w.TLSCertsDir, 0755); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	caCertData, err := base64.StdEncoding.DecodeString(w.TLSCACert)
	if err != nil {
		return fmt.Errorf("failed to decode CA cert: %w", err)
	}

	clientCertData, err := base64.StdEncoding.DecodeString(w.TLSClientCert)
	if err != nil {
		return fmt.Errorf("failed to decode client cert: %w", err)
	}

	clientKeyData, err := base64.StdEncoding.DecodeString(w.TLSClientKey)
	if err != nil {
		return fmt.Errorf("failed to decode client key: %w", err)
	}

	caPath := filepath.Join(w.TLSCertsDir, "ca.pem")
	if err := os.WriteFile(caPath, caCertData, 0644); err != nil {
		return fmt.Errorf("failed to write CA cert: %w", err)
	}

	clientCertPath := filepath.Join(w.TLSCertsDir, "client.pem")
	if err := os.WriteFile(clientCertPath, clientCertData, 0644); err != nil {
		return fmt.Errorf("failed to write client cert: %w", err)
	}

	clientKeyPath := filepath.Join(w.TLSCertsDir, "client-key.pem")
	if err := os.WriteFile(clientKeyPath, clientKeyData, 0600); err != nil {
		return fmt.Errorf("failed to write client key: %w", err)
	}

	return nil
}

func (w *WireGuardConfig) GetCACertPath() string {
	return filepath.Join(w.TLSCertsDir, "ca.pem")
}

func (w *WireGuardConfig) GetClientCertPath() string {
	return filepath.Join(w.TLSCertsDir, "client.pem")
}

func (w *WireGuardConfig) GetClientKeyPath() string {
	return filepath.Join(w.TLSCertsDir, "client-key.pem")
}
