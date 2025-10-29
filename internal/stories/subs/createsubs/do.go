package createsubs

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"kurut-bot/internal/marzban"
	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	"github.com/pkg/errors"
)

type Service struct {
	storage    storage
	marzbanSvc marzbanService
	now        func() time.Time
}

func NewService(storage storage, marzbanSvc marzbanService, now func() time.Time) *Service {
	return &Service{
		storage:    storage,
		marzbanSvc: marzbanSvc,
		now:        now,
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

	marzbanSub, err := s.getMarzbanSub(ctx, req.UserID, expiresAt, tariff.TrafficLimitGB)
	if err != nil {
		return nil, err
	}

	subscription := subs.Subscription{
		UserID:        req.UserID,
		TariffID:      req.TariffID,
		MarzbanUserID: marzbanSub.MarzbanUserID,
		MarzbanLink:   marzbanSub.SubscriptionURL,
		Status:        subs.StatusActive,
		ClientName:    req.ClientName,
		ActivatedAt:   &now,
		ExpiresAt:     &expiresAt,
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

func (s *Service) getMarzbanSub(ctx context.Context, userID int64, expiresAt time.Time, trafficLimitGB *int) (*marzban.UserSubscription, error) {
	now := s.now()
	marzbanUserID := fmt.Sprintf("user_%d_%d_%d", userID, now.Unix(), rand.Intn(1000000))

	protocols := []string{
		marzban.ProtocolVLESS,
		marzban.ProtocolTrojan,
	}

	return s.marzbanSvc.CreateUser(ctx, marzban.CreateUserRequest{
		Username:       marzbanUserID,
		Protocols:      protocols,
		ExpiresAt:      expiresAt,
		TrafficLimitGB: trafficLimitGB,
	})
}
