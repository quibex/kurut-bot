package orders

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreatePendingOrder(ctx context.Context, order PendingOrder) (*PendingOrder, error) {
	return s.repo.CreatePendingOrder(ctx, order)
}

func (s *Service) GetPendingOrderByID(ctx context.Context, id int64) (*PendingOrder, error) {
	return s.repo.GetPendingOrderByID(ctx, id)
}

func (s *Service) UpdateMessageID(ctx context.Context, id int64, messageID int) error {
	return s.repo.UpdatePendingOrderMessageID(ctx, id, messageID)
}

func (s *Service) UpdatePaymentID(ctx context.Context, id int64, paymentID int64) error {
	return s.repo.UpdatePendingOrderPaymentID(ctx, id, paymentID)
}

func (s *Service) UpdateStatus(ctx context.Context, id int64, status Status) error {
	return s.repo.UpdatePendingOrderStatus(ctx, id, status)
}

func (s *Service) DeletePendingOrder(ctx context.Context, id int64) error {
	return s.repo.DeletePendingOrder(ctx, id)
}
