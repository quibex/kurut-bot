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

func (s *Service) CreateSubscription(ctx context.Context, req *subs.CreateSubscriptionRequest) (*subs.Subscription, error) {
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

	// Create user in Marzban
	marzbanUserID := fmt.Sprintf("user_%d_%d", req.UserID, now.Unix())

	userCreate := &marzban.UserCreate{
		Username: marzbanUserID,
	}

	// Set up proxies
	proxies := marzban.OptUserCreateProxies{}
	proxySettings := make(marzban.UserCreateProxies)
	proxySettings["vless"] = marzban.ProxySettings{}
	proxies.SetTo(proxySettings)
	userCreate.Proxies = proxies

	// Set up inbounds
	inbounds := marzban.OptUserCreateInbounds{}
	inboundSettings := make(marzban.UserCreateInbounds)
	inboundSettings["vless"] = vlessInbounds
	inbounds.SetTo(inboundSettings)
	userCreate.Inbounds = inbounds

	// Set expire time
	expire := marzban.OptNilInt{}
	expire.SetTo(int(expiresAt.Unix()))
	userCreate.Expire = expire

	// Set data limit if specified
	if tariff.TrafficLimitGB != nil {
		dataLimit := marzban.OptInt{}
		dataLimit.SetTo(int(*tariff.TrafficLimitGB * 1024 * 1024 * 1024))
		userCreate.DataLimit = dataLimit
	}

	// Set status to active
	status := marzban.OptUserStatusCreate{}
	status.SetTo(marzban.UserStatusCreateActive)
	userCreate.Status = status

	// Create user in Marzban
	addUserRes, err := s.marzbanClient.AddUser(ctx, userCreate)
	if err != nil {
		return nil, errors.Errorf("failed to create user in Marzban: %v", err)
	}

	// Extract subscription URL
	var subscriptionURL string
	switch res := addUserRes.(type) {
	case *marzban.UserResponse:
		if res.GetSubscriptionURL().Set {
			rawURL := res.GetSubscriptionURL().Value
			subscriptionURL = s.buildFullSubscriptionURL(rawURL)
		}
	case *marzban.HTTPException:
		return nil, errors.Errorf("Marzban API error: %s", res.GetDetail())
	case *marzban.Conflict:
		return nil, errors.Errorf("Marzban conflict error (user may already exist)")
	case *marzban.HTTPValidationError:
		return nil, errors.Errorf("Marzban validation error: invalid request")
	case *marzban.UnauthorizedHeaders:
		return nil, errors.Errorf("Marzban authorization error: check API token")
	default:
		return nil, errors.Errorf("unexpected response from Marzban AddUser: %T", addUserRes)
	}

	// Create subscription in database
	subscription := subs.Subscription{
		UserID:        req.UserID,
		TariffID:      req.TariffID,
		MarzbanUserID: marzbanUserID,
		MarzbanLink:   subscriptionURL,
		Status:        subs.StatusActive,
		ActivatedAt:   &now,
		ExpiresAt:     &expiresAt,
	}

	created, err := s.storage.CreateSubscription(ctx, subscription)
	if err != nil {
		return nil, errors.Errorf("failed to create subscription in database: %v", err)
	}

	return created, nil
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
