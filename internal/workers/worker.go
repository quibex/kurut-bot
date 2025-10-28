package workers

// Worker defines the interface for all background workers
type Worker interface {
	// Start starts the worker
	Start() error

	// Stop gracefully stops the worker
	Stop()

	// Name returns the worker name for logging
	Name() string
}




