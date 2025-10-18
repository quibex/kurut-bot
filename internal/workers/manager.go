package workers

import (
	"fmt"
	"log/slog"
)

// Manager manages multiple workers
type Manager struct {
	workers []Worker
	logger  *slog.Logger
}

// NewManager creates a new worker manager
func NewManager(logger *slog.Logger, workers ...Worker) *Manager {
	return &Manager{
		workers: workers,
		logger:  logger,
	}
}

// Start starts all workers
func (m *Manager) Start() error {
	m.logger.Info("Starting worker manager", "worker_count", len(m.workers))

	for _, worker := range m.workers {
		m.logger.Info("Starting worker", "name", worker.Name())
		if err := worker.Start(); err != nil {
			return fmt.Errorf("failed to start worker %s: %w", worker.Name(), err)
		}
		m.logger.Info("Worker started successfully", "name", worker.Name())
	}

	m.logger.Info("All workers started successfully")
	return nil
}

// Stop stops all workers
func (m *Manager) Stop() {
	m.logger.Info("Stopping all workers")

	for _, worker := range m.workers {
		m.logger.Info("Stopping worker", "name", worker.Name())
		worker.Stop()
	}

	m.logger.Info("All workers stopped")
}



