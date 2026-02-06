package storage

import (
	"context"
	"fmt"

	"kurut-bot/internal/stories/orders"
	"kurut-bot/internal/stories/tariffs"
	"kurut-bot/internal/web"
)

// CreatePendingOrderFromWeb implements web.OrderStorage interface
// It converts web.PendingOrder to orders.PendingOrder and saves it
func (s *storageImpl) CreatePendingOrderFromWeb(ctx context.Context, order web.PendingOrder) (*web.PendingOrder, error) {
	// Get tariff to fill in name and amount
	tariff, err := s.GetTariff(ctx, tariffs.GetCriteria{ID: &order.TariffID})
	if err != nil {
		return nil, fmt.Errorf("get tariff: %w", err)
	}
	if tariff == nil {
		return nil, fmt.Errorf("tariff not found: %d", order.TariffID)
	}

	// Convert web.PendingOrder to orders.PendingOrder
	fullOrder := orders.PendingOrder{
		PaymentID:              order.PaymentID,
		AdminUserID:            0, // Not applicable for web orders
		AssistantTelegramID:    order.CreatedByTelegramID,
		ChatID:                 0, // Not applicable for web orders
		MessageID:              nil,
		ClientWhatsApp:         order.ClientWhatsApp,
		ServerID:               nil,
		ServerName:             nil,
		TariffID:               order.TariffID,
		TariffName:             tariff.Name,
		TotalAmount:            tariff.Price,
		ReferrerWhatsApp:       order.ReferrerWhatsApp,
		ReferrerSubscriptionID: nil, // Will be set by worker
		Status:                 orders.StatusPending,
	}

	// Create the order using existing method
	created, err := s.CreatePendingOrder(ctx, fullOrder)
	if err != nil {
		return nil, err
	}

	// Convert back to web.PendingOrder
	return &web.PendingOrder{
		ID:                  created.ID,
		ClientWhatsApp:      created.ClientWhatsApp,
		TariffID:            created.TariffID,
		ReferrerWhatsApp:    created.ReferrerWhatsApp,
		PaymentID:           created.PaymentID,
		CreatedByTelegramID: created.AssistantTelegramID,
	}, nil
}

// CancelPendingOrdersByWhatsApp cancels all pending orders for a client and returns payment IDs
func (s *storageImpl) CancelPendingOrdersByWhatsApp(ctx context.Context, whatsapp string) ([]int64, error) {
	// Get all pending orders with payments
	pendingOrders, err := s.ListPendingOrdersWithPayments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending orders: %w", err)
	}

	var paymentIDs []int64
	for _, order := range pendingOrders {
		if order.ClientWhatsApp == whatsapp {
			paymentIDs = append(paymentIDs, order.PaymentID)
			// Delete the pending order
			if err := s.DeletePendingOrder(ctx, order.ID); err != nil {
				return nil, fmt.Errorf("delete pending order %d: %w", order.ID, err)
			}
		}
	}

	return paymentIDs, nil
}
