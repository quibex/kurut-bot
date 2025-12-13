package orders

import "context"

type Repository interface {
	CreatePendingOrder(ctx context.Context, order PendingOrder) (*PendingOrder, error)
	GetPendingOrderByID(ctx context.Context, id int64) (*PendingOrder, error)
	UpdatePendingOrderMessageID(ctx context.Context, id int64, messageID int) error
	UpdatePendingOrderPaymentID(ctx context.Context, id int64, paymentID int64) error
	UpdatePendingOrderStatus(ctx context.Context, id int64, status Status) error
	DeletePendingOrder(ctx context.Context, id int64) error
}
