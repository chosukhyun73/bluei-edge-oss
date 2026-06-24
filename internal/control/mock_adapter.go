package control

import (
	"context"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/config"
)

// MockAdapter simulates device command execution with a configurable delay.
type MockAdapter struct {
	cfg config.ControlAdapter
}

func NewMockAdapter(cfg config.ControlAdapter) *MockAdapter {
	return &MockAdapter{cfg: cfg}
}

// Execute simulates sending a command to a mock device and returns success.
func (a *MockAdapter) Execute(ctx context.Context, commandID, commandType string, params map[string]any) (ack map[string]any, err error) {
	timeout := time.Duration(a.cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Simulate minimal async work
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}

	slog.Info("mock adapter executed command", "adapter", a.cfg.ID, "command_id", commandID, "type", commandType)
	return map[string]any{
		"ack_id": "mock-ack-" + commandID,
		"state":  "completed",
	}, nil
}
