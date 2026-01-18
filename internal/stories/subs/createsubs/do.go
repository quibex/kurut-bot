package createsubs

import (
	"context"
	"time"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"

	"github.com/pkg/errors"
)

type Service struct {
	storage storage
	now     func() time.Time
}

func NewService(storage storage, now func() time.Time) *Service {
	return &Service{
		storage: storage,
		now:     now,
	}
}

const referralBonusDays = 10

func (s *Service) CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.CreateSubscriptionResult, error) {
	tariff, err := s.storage.GetTariff(ctx, tariffs.GetCriteria{ID: &req.TariffID})
	if err != nil {
		return nil, errors.Errorf("failed to get tariff: %v", err)
	}
	if tariff == nil {
		return nil, errors.Errorf("tariff not found")
	}

	// Получаем доступный сервер
	server, err := s.storage.GetAvailableServer(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to get available server: %v", err)
	}
	if server == nil {
		return nil, errors.Errorf("no available servers")
	}

	now := s.now()

	// No bonus for the new client - only the referrer gets bonus
	durationDays := tariff.DurationDays
	var referralBonusApplied bool
	var referrerWhatsApp *string

	if req.ReferrerSubscriptionID != nil {
		// Get referrer's WhatsApp for display in success message
		referrerSub, _ := s.storage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{*req.ReferrerSubscriptionID}})
		if referrerSub != nil {
			referrerWhatsApp = referrerSub.ClientWhatsApp
		}

		// Bonus is only given for PAID subscriptions AND only for the client's FIRST paid subscription
		if req.PaymentID != nil {
			hasPaidSub, err := s.storage.HasPaidSubscriptionByPhone(ctx, req.ClientWhatsApp)
			if err == nil && !hasPaidSub {
				referralBonusApplied = true
			}
		}
	}

	expiresAt := now.AddDate(0, 0, durationDays)

	subscription := subs.Subscription{
		UserID:              req.UserID,
		TariffID:            req.TariffID,
		ServerID:            &server.ID,
		Status:              subs.StatusActive,
		ClientWhatsApp:      &req.ClientWhatsApp,
		CreatedByTelegramID: &req.CreatedByTelegramID,
		ReferrerWhatsApp:    referrerWhatsApp,
		ActivatedAt:         &now,
		ExpiresAt:           &expiresAt,
	}

	created, err := s.storage.CreateSubscription(ctx, subscription)
	if err != nil {
		return nil, errors.Errorf("failed to create subscription in database: %v", err)
	}

	// Счетчик пользователей на сервере теперь считается динамически (не нужен инкремент)

	// Генерируем user_id после создания подписки (когда уже есть ID)
	generatedUserID := subs.GenerateUserID(created.ID, req.CreatedByTelegramID, req.ClientWhatsApp)

	// Обновляем подписку с generated_user_id
	err = s.storage.UpdateSubscriptionGeneratedUserID(ctx, created.ID, generatedUserID)
	if err != nil {
		return nil, errors.Errorf("failed to update subscription with generated user id: %v", err)
	}
	created.GeneratedUserID = &generatedUserID

	if req.PaymentID != nil {
		err = s.storage.LinkPaymentToSubscriptions(ctx, *req.PaymentID, []int64{created.ID})
		if err != nil {
			return nil, errors.Errorf("failed to link payment to subscription: %v", err)
		}
	}

	// Extend referrer's subscription if referral bonus was applied
	var referrerNewExpiresAt *time.Time
	var referrerWeeklyCount int
	if referralBonusApplied && req.ReferrerSubscriptionID != nil {
		if err := s.storage.ExtendSubscription(ctx, *req.ReferrerSubscriptionID, referralBonusDays); err == nil {
			// Get updated referrer subscription to get new expiry date
			updatedReferrerSub, _ := s.storage.GetSubscription(ctx, subs.GetCriteria{IDs: []int64{*req.ReferrerSubscriptionID}})
			if updatedReferrerSub != nil {
				referrerNewExpiresAt = updatedReferrerSub.ExpiresAt
			}
		}
		// Count weekly referrals for this referrer (including the one just created)
		if referrerWhatsApp != nil {
			count, _ := s.storage.CountWeeklyReferrals(ctx, *referrerWhatsApp)
			referrerWeeklyCount = count
		}
	}

	return &subs.CreateSubscriptionResult{
		Subscription:         created,
		GeneratedUserID:      generatedUserID,
		ServerUIURL:          &server.UIURL,
		ServerUIPassword:     &server.UIPassword,
		ReferralBonusApplied: referralBonusApplied,
		ReferrerWhatsApp:     referrerWhatsApp,
		ReferrerNewExpiresAt: referrerNewExpiresAt,
		ReferrerWeeklyCount:  referrerWeeklyCount,
	}, nil
}

// FindActiveSubscriptionByWhatsApp finds an active subscription by client WhatsApp number
func (s *Service) FindActiveSubscriptionByWhatsApp(ctx context.Context, whatsapp string) (*subs.Subscription, error) {
	return s.storage.FindActiveSubscriptionByWhatsApp(ctx, whatsapp)
}

// MigrateSubscription создаёт подписку для существующего клиента БЕЗ увеличения счётчика сервера
func (s *Service) MigrateSubscription(ctx context.Context, req *subs.MigrateSubscriptionRequest) (*subs.CreateSubscriptionResult, error) {
	tariff, err := s.storage.GetTariff(ctx, tariffs.GetCriteria{ID: &req.TariffID})
	if err != nil {
		return nil, errors.Errorf("failed to get tariff: %v", err)
	}
	if tariff == nil {
		return nil, errors.Errorf("tariff not found")
	}

	// Получаем выбранный сервер
	server, err := s.storage.GetServerByID(ctx, req.ServerID)
	if err != nil {
		return nil, errors.Errorf("failed to get server: %v", err)
	}
	if server == nil {
		return nil, errors.Errorf("server not found")
	}

	now := s.now()
	expiresAt := now.AddDate(0, 0, tariff.DurationDays)

	subscription := subs.Subscription{
		UserID:              req.UserID,
		TariffID:            req.TariffID,
		ServerID:            &server.ID,
		Status:              subs.StatusActive,
		ClientWhatsApp:      &req.ClientWhatsApp,
		CreatedByTelegramID: &req.CreatedByTelegramID,
		ActivatedAt:         &now,
		ExpiresAt:           &expiresAt,
	}

	created, err := s.storage.CreateSubscription(ctx, subscription)
	if err != nil {
		return nil, errors.Errorf("failed to create subscription in database: %v", err)
	}

	// НЕ увеличиваем счетчик пользователей на сервере - клиент уже там есть

	// Генерируем user_id после создания подписки
	generatedUserID := subs.GenerateUserID(created.ID, req.CreatedByTelegramID, req.ClientWhatsApp)

	// Обновляем подписку с generated_user_id
	err = s.storage.UpdateSubscriptionGeneratedUserID(ctx, created.ID, generatedUserID)
	if err != nil {
		return nil, errors.Errorf("failed to update subscription with generated user id: %v", err)
	}
	created.GeneratedUserID = &generatedUserID

	return &subs.CreateSubscriptionResult{
		Subscription:     created,
		GeneratedUserID:  generatedUserID,
		ServerUIURL:      &server.UIURL,
		ServerUIPassword: &server.UIPassword,
	}, nil
}
