package wgserver

import "kurut-bot/internal/config"

type tlsConfigAdapter struct {
	cfg *config.WireGuardConfig
}

func NewTLSConfigAdapter(cfg *config.WireGuardConfig) TLSConfig {
	return &tlsConfigAdapter{cfg: cfg}
}

func (a *tlsConfigAdapter) GetCACertPath() string {
	return a.cfg.GetCACertPath()
}

func (a *tlsConfigAdapter) GetClientCertPath() string {
	return a.cfg.GetClientCertPath()
}

func (a *tlsConfigAdapter) GetClientKeyPath() string {
	return a.cfg.GetClientKeyPath()
}

func (a *tlsConfigAdapter) GetServerName() string {
	return a.cfg.TLSServerName
}

