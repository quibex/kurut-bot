package createsubs

import (
	"context"
	"fmt"
	"time"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	"github.com/pkg/errors"
)

type Service struct {
	storage       storage
	wireguardSvc  wireguardService
	now           func() time.Time
}

func NewService(storage storage, wireguardSvc wireguardService, now func() time.Time) *Service {
	return &Service{
		storage:      storage,
		wireguardSvc: wireguardSvc,
		now:          now,
	}
}

func (s *Service) CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.Subscription, error) {
	tariff, err := s.storage.GetTariff(ctx, tariffs.GetCriteria{ID: &req.TariffID})
	if err != nil {
		return nil, errors.Errorf("failed to get tariff: %v", err)
	}
	if tariff == nil {
		return nil, errors.Errorf("tariff not found")
	}

	expiresAt := s.now().AddDate(0, 0, tariff.DurationDays)
	now := s.now()

	peerID := fmt.Sprintf("user_%d_%d", req.UserID, now.Unix())
	peerConfig, err := s.wireguardSvc.CreatePeer(ctx, req.UserID, peerID)
	if err != nil {
		return nil, errors.Errorf("failed to create wireguard peer: %v", err)
	}

	wgData := subs.WireGuardData{
		ServerID:  peerConfig.ServerID,
		PublicKey: peerConfig.PublicKey,
		AllowedIP: peerConfig.AllowedIP,
		Config:    peerConfig.Config,
		QRCode:    peerConfig.QRCode,
	}

	vpnData, err := subs.MarshalWireGuardData(wgData)
	if err != nil {
		return nil, errors.Errorf("failed to marshal wireguard data: %v", err)
	}

	subscription := subs.Subscription{
		UserID:      req.UserID,
		TariffID:    req.TariffID,
		VPNType:     string(subs.VPNTypeWireGuard),
		VPNData:     vpnData,
		Status:      subs.StatusActive,
		ClientName:  req.ClientName,
		ActivatedAt: &now,
		ExpiresAt:   &expiresAt,
	}

	created, err := s.storage.CreateSubscription(ctx, subscription)
	if err != nil {
		return nil, errors.Errorf("failed to create subscription in database: %v", err)
	}

	if req.PaymentID != nil {
		err = s.storage.LinkPaymentToSubscriptions(ctx, *req.PaymentID, []int64{created.ID})
		if err != nil {
			return nil, errors.Errorf("failed to link payment to subscription: %v", err)
		}
	}

	return created, nil
}
