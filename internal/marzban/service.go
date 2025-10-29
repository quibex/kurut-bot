package marzban

import (
	"context"
	"strings"
	"time"

	marzbanAPI "kurut-bot/pkg/marzban"

	"github.com/pkg/errors"
)

type Service struct {
	client  Client
	baseURL string
}

func NewService(client Client, baseURL string) *Service {
	return &Service{
		client:  client,
		baseURL: baseURL,
	}
}

func (s *Service) CreateUser(ctx context.Context, req CreateUserRequest) (*UserSubscription, error) {
	protocolsConfig := make([]ProtocolConfig, 0, len(req.Protocols))

	for _, protocol := range req.Protocols {
		inbounds, err := s.getInboundsForProtocol(ctx, protocol)
		if err != nil {
			return nil, errors.Errorf("failed to get %s inbounds: %v", protocol, err)
		}
		protocolsConfig = append(protocolsConfig, ProtocolConfig{
			Name:     protocol,
			Inbounds: inbounds,
		})
	}

	subscriptionURL, err := s.createMarzbanUser(ctx, req.Username, protocolsConfig, req.ExpiresAt, req.TrafficLimitGB)
	if err != nil {
		return nil, err
	}

	return &UserSubscription{
		MarzbanUserID:   req.Username,
		SubscriptionURL: subscriptionURL,
	}, nil
}

func (s *Service) createMarzbanUser(ctx context.Context, username string, protocols []ProtocolConfig, expiresAt time.Time, trafficLimitGB *int) (string, error) {
	userCreate := &marzbanAPI.UserCreate{
		Username: username,
	}

	proxies := marzbanAPI.OptUserCreateProxies{}
	proxySettings := make(marzbanAPI.UserCreateProxies)

	inbounds := marzbanAPI.OptUserCreateInbounds{}
	inboundSettings := make(marzbanAPI.UserCreateInbounds)

	for _, protocol := range protocols {
		proxySettings[protocol.Name] = marzbanAPI.ProxySettings{}
		inboundSettings[protocol.Name] = protocol.Inbounds
	}

	proxies.SetTo(proxySettings)
	userCreate.Proxies = proxies

	inbounds.SetTo(inboundSettings)
	userCreate.Inbounds = inbounds

	expire := marzbanAPI.OptNilInt{}
	expire.SetTo(int(expiresAt.Unix()))
	userCreate.Expire = expire

	if trafficLimitGB != nil {
		dataLimit := marzbanAPI.OptInt{}
		dataLimit.SetTo(*trafficLimitGB * 1024 * 1024 * 1024)
		userCreate.DataLimit = dataLimit
	}

	status := marzbanAPI.OptUserStatusCreate{}
	status.SetTo(marzbanAPI.UserStatusCreateActive)
	userCreate.Status = status

	addUserRes, err := s.client.AddUser(ctx, userCreate)
	if err != nil {
		return "", errors.Errorf("failed to create user in Marzban: %v", err)
	}

	var subscriptionURL string
	switch res := addUserRes.(type) {
	case *marzbanAPI.UserResponse:
		if res.GetSubscriptionURL().Set {
			rawURL := res.GetSubscriptionURL().Value
			subscriptionURL = s.buildFullSubscriptionURL(rawURL)
		}
	case *marzbanAPI.HTTPException:
		return "", errors.Errorf("Marzban API error: %s", res.GetDetail())
	case *marzbanAPI.Conflict:
		return "", errors.Errorf("Marzban conflict error (user may already exist)")
	case *marzbanAPI.HTTPValidationError:
		return "", errors.Errorf("Marzban validation error: invalid request")
	case *marzbanAPI.UnauthorizedHeaders:
		return "", errors.Errorf("Marzban authorization error: check API token")
	default:
		return "", errors.Errorf("unexpected response from Marzban AddUser: %T", addUserRes)
	}

	return subscriptionURL, nil
}

func (s *Service) buildFullSubscriptionURL(subscriptionURL string) string {
	if strings.HasPrefix(subscriptionURL, "http://") || strings.HasPrefix(subscriptionURL, "https://") {
		return subscriptionURL
	}

	if strings.HasPrefix(subscriptionURL, "/") {
		baseURL := strings.TrimSuffix(s.baseURL, "/")
		return baseURL + subscriptionURL
	}

	baseURL := strings.TrimSuffix(s.baseURL, "/")
	return baseURL + "/" + subscriptionURL
}

func (s *Service) UpdateUserExpiry(ctx context.Context, marzbanUserID string, newExpiresAt time.Time) error {
	userModify := &marzbanAPI.UserModify{}

	expire := marzbanAPI.OptNilInt{}
	expire.SetTo(int(newExpiresAt.Unix()))
	userModify.Expire = expire

	params := marzbanAPI.ModifyUserParams{
		Username: marzbanUserID,
	}

	res, err := s.client.ModifyUser(ctx, userModify, params)
	if err != nil {
		return errors.Errorf("failed to modify user in Marzban: %v", err)
	}

	switch r := res.(type) {
	case *marzbanAPI.UserResponse:
		return nil
	case *marzbanAPI.HTTPException:
		return errors.Errorf("Marzban API error: %s", r.GetDetail())
	case *marzbanAPI.NotFound:
		return errors.Errorf("Marzban user not found: %s", marzbanUserID)
	case *marzbanAPI.HTTPValidationError:
		return errors.Errorf("Marzban validation error: invalid request")
	case *marzbanAPI.UnauthorizedHeaders:
		return errors.Errorf("Marzban authorization error: check API token")
	default:
		return errors.Errorf("unexpected response from Marzban ModifyUser: %T", res)
	}
}

func (s *Service) getInboundsForProtocol(ctx context.Context, protocolName string) ([]string, error) {
	inboundsRes, err := s.client.GetInbounds(ctx)
	if err != nil {
		return nil, errors.Errorf("failed to get inbounds: %v", err)
	}

	var protocolInbounds []string

	switch res := inboundsRes.(type) {
	case *marzbanAPI.GetInboundsOK:
		if inbounds, ok := (*res)[protocolName]; ok {
			for _, inbound := range inbounds {
				protocolInbounds = append(protocolInbounds, inbound.Tag)
			}
		}
	default:
		return nil, errors.Errorf("unexpected response from GetInbounds: %T", res)
	}

	if len(protocolInbounds) == 0 {
		fallbackNames := map[string]string{
			ProtocolVLESS:  InboundVLESSDefault,
			ProtocolTrojan: InboundTrojanDefault,
		}
		if fallbackName, ok := fallbackNames[protocolName]; ok {
			protocolInbounds = []string{fallbackName}
		}
	}

	return protocolInbounds, nil
}
