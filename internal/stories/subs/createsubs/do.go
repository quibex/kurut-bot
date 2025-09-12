package createsubs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kurut-bot/internal/stories/subs"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/pkg/marzban"

	"github.com/pkg/errors"
	"github.com/samber/lo"
)

type Service struct {
	storage        storage
	marzbanClient  marzbanClient
	now            func() time.Time
	marzbanBaseURL string
}

func NewService(storage storage, marzbanClient marzbanClient, now func() time.Time, marzbanBaseURL string) *Service {
	return &Service{
		storage:        storage,
		marzbanClient:  marzbanClient,
		now:            now,
		marzbanBaseURL: marzbanBaseURL,
	}
}

func (s *Service) CreateSubscriptions(ctx context.Context, req *subs.CreateSubscriptionsRequest) ([]subs.Subscription, error) {
	// Get tariff information
	tariff, err := s.storage.GetTariff(ctx, tariffs.GetCriteria{ID: &req.TariffID})
	if err != nil {
		return nil, errors.Errorf("failed to get tariff: %v", err)
	}
	if tariff == nil {
		return nil, errors.Errorf("tariff not found")
	}

	// Calculate expiration date
	expiresAt := s.now().AddDate(0, 0, tariff.DurationDays)
	now := s.now()

	// Get available VLESS inbounds
	vlessInbounds, err := s.getVlessInbounds(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to get VLESS inbounds: %v", err)
	}

	// Create users in Marzban first (one by one, no bulk API available)
	type createdUser struct {
		username        string
		subscriptionURL string
	}
	createdUsers := make([]createdUser, 0, req.Quantity)

	for i := 0; i < req.Quantity; i++ {
		marzbanUserID := fmt.Sprintf("user_%d_%d", req.UserID, now.Unix())

		// Create user in Marzban
		userCreate := &marzban.UserCreate{
			Username: marzbanUserID,
		}

		// Set up proxies - at least one protocol is required
		proxies := marzban.OptUserCreateProxies{}
		proxySettings := make(marzban.UserCreateProxies)
		proxySettings["vless"] = marzban.ProxySettings{} // Enable vless protocol
		proxies.SetTo(proxySettings)
		userCreate.Proxies = proxies

		// Set up inbounds for protocols using dynamic inbound discovery
		inbounds := marzban.OptUserCreateInbounds{}
		inboundSettings := make(marzban.UserCreateInbounds)
		inboundSettings["vless"] = vlessInbounds // Use dynamically discovered VLESS inbounds
		inbounds.SetTo(inboundSettings)
		userCreate.Inbounds = inbounds

		// Set expire time (Unix timestamp)
		expire := marzban.OptNilInt{}
		expire.SetTo(int(expiresAt.Unix()))
		userCreate.Expire = expire

		// Set data limit if specified in tariff
		if tariff.TrafficLimitGB != nil {
			dataLimit := marzban.OptInt{}
			dataLimit.SetTo(int(*tariff.TrafficLimitGB * 1024 * 1024 * 1024)) // Convert GB to bytes
			userCreate.DataLimit = dataLimit
		}

		// Set status to active
		status := marzban.OptUserStatusCreate{}
		status.SetTo(marzban.UserStatusCreateActive)
		userCreate.Status = status

		// Create user in Marzban
		addUserRes, err := s.marzbanClient.AddUser(ctx, userCreate)
		if err != nil {
			return nil, errors.Errorf("failed to create user %s in Marzban: %v", marzbanUserID, err)
		}

		// Check if user creation was successful
		switch res := addUserRes.(type) {
		case *marzban.UserResponse:
			// User created successfully, extract subscription URL
			subscriptionURL := ""
			if res.GetSubscriptionURL().Set {
				rawURL := res.GetSubscriptionURL().Value
				// Формируем полную ссылку
				subscriptionURL = s.buildFullSubscriptionURL(rawURL)
			}

			// Store both username and subscription URL for later use
			createdUsers = append(createdUsers, createdUser{
				username:        marzbanUserID,
				subscriptionURL: subscriptionURL,
			})
		case *marzban.HTTPException:
			return nil, errors.Errorf("Marzban API error for user %s: %s", marzbanUserID, res.GetDetail())
		case *marzban.Conflict:
			return nil, errors.Errorf("Marzban conflict error for user %s (user may already exist)", marzbanUserID)
		case *marzban.HTTPValidationError:
			return nil, errors.Errorf("Marzban validation error for user %s: invalid request", marzbanUserID)
		case *marzban.UnauthorizedHeaders:
			return nil, errors.Errorf("Marzban authorization error for user %s: check API token", marzbanUserID)
		default:
			return nil, errors.Errorf("unexpected response from Marzban AddUser for user %s: %T", marzbanUserID, addUserRes)
		}
	}

	// All users created successfully in Marzban, now create subscriptions in database
	subscriptions := lo.Map(createdUsers, func(user createdUser, _ int) subs.Subscription {
		return subs.Subscription{
			UserID:        req.UserID,
			TariffID:      req.TariffID,
			MarzbanUserID: user.username,
			MarzbanLink:   user.subscriptionURL,
			Status:        subs.StatusActive,
			ActivatedAt:   &now,
			ExpiresAt:     &expiresAt,
		}
	})

	// Create subscriptions in database
	subscriptions, err = s.storage.BulkInsertSubscriptions(ctx, subscriptions)
	if err != nil {
		return nil, errors.Errorf("failed to create subscriptions in database: %v", err)
	}

	return subscriptions, nil
}

// buildFullSubscriptionURL формирует полную ссылку подписки
func (s *Service) buildFullSubscriptionURL(subscriptionURL string) string {
	// Если ссылка уже полная (содержит http:// или https://), возвращаем как есть
	if strings.HasPrefix(subscriptionURL, "http://") || strings.HasPrefix(subscriptionURL, "https://") {
		return subscriptionURL
	}

	// Если ссылка относительная (начинается с /), добавляем базовый URL
	if strings.HasPrefix(subscriptionURL, "/") {
		baseURL := strings.TrimSuffix(s.marzbanBaseURL, "/")
		return baseURL + subscriptionURL
	}

	// Если ссылка не начинается с /, добавляем и базовый URL и /
	baseURL := strings.TrimSuffix(s.marzbanBaseURL, "/")
	return baseURL + "/" + subscriptionURL
}

// getVlessInbounds получает подходящие inbound'ы для VLESS протокола
func (s *Service) getVlessInbounds(ctx context.Context) ([]string, error) {
	// Получаем список всех доступных inbound'ов
	inboundsRes, err := s.marzbanClient.GetInbounds(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to get inbounds: %v", err)
	}

	var vlessInbounds []string

	// Обрабатываем ответ
	switch res := inboundsRes.(type) {
	case *marzban.GetInboundsOK:
		// Ищем inbound'ы для vless протокола
		if inbounds, ok := (*res)["vless"]; ok {
			for _, inbound := range inbounds {
				// Добавляем тег inbound'а
				vlessInbounds = append(vlessInbounds, inbound.Tag)
			}
		}
	default:
		return nil, errors.Errorf("unexpected response from GetInbounds: %T", res)
	}

	// Если не нашли VLESS inbound'ы, используем стандартные имена
	if len(vlessInbounds) == 0 {
		vlessInbounds = []string{"VLESS TCP REALITY"} // Fallback to common names
	}

	return vlessInbounds, nil
}
