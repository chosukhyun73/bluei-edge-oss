package runtime

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Service is the interface all background workers implement.
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Manager starts and stops a set of services in order.
type Manager struct {
	services []Service
	mu       sync.Mutex
	started  []Service
}

func NewManager(svcs ...Service) *Manager {
	return &Manager{services: svcs}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.services {
		slog.Info("starting service", "service", s.Name())
		if err := s.Start(ctx); err != nil {
			return err
		}
		m.started = append(m.started, s)
	}
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// stop in reverse order
	for i := len(m.started) - 1; i >= 0; i-- {
		s := m.started[i]
		slog.Info("stopping service", "service", s.Name())
		if err := s.Stop(ctx); err != nil {
			slog.Warn("service stop error", "service", s.Name(), "error", err)
		}
	}
}
