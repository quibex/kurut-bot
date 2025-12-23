package servers

import (
	"context"

	"github.com/pkg/errors"
)

type Service struct {
	storage Storage
}

func NewService(storage Storage) *Service {
	return &Service{
		storage: storage,
	}
}

func (s *Service) CreateServer(ctx context.Context, server Server) (*Server, error) {
	created, err := s.storage.CreateServer(ctx, server)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create server in storage")
	}

	return created, nil
}

func (s *Service) GetServer(ctx context.Context, criteria GetCriteria) (*Server, error) {
	server, err := s.storage.GetServer(ctx, criteria)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get server from storage")
	}

	return server, nil
}

func (s *Service) ListServers(ctx context.Context, criteria ListCriteria) ([]*Server, error) {
	servers, err := s.storage.ListServers(ctx, criteria)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list servers from storage")
	}

	return servers, nil
}

func (s *Service) UpdateServer(ctx context.Context, criteria GetCriteria, params UpdateParams) (*Server, error) {
	updated, err := s.storage.UpdateServer(ctx, criteria, params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to update server in storage")
	}

	return updated, nil
}

func (s *Service) ArchiveServer(ctx context.Context, serverID int64) (*Server, error) {
	archived := true
	updated, err := s.storage.UpdateServer(ctx, GetCriteria{ID: &serverID}, UpdateParams{Archived: &archived})
	if err != nil {
		return nil, errors.Wrap(err, "failed to archive server")
	}

	return updated, nil
}

func (s *Service) UnarchiveServer(ctx context.Context, serverID int64) (*Server, error) {
	archived := false
	updated, err := s.storage.UpdateServer(ctx, GetCriteria{ID: &serverID}, UpdateParams{Archived: &archived})
	if err != nil {
		return nil, errors.Wrap(err, "failed to unarchive server")
	}

	return updated, nil
}

func (s *Service) DecrementServerUsers(ctx context.Context, serverID int64) error {
	err := s.storage.DecrementServerUsers(ctx, serverID)
	if err != nil {
		return errors.Wrap(err, "failed to decrement server users")
	}

	return nil
}
