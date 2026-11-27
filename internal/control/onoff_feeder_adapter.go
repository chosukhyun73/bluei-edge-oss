package control

import (
	"context"
	"errors"

	"bluei.kr/edge/internal/config"
)

// ErrOnoffFeederUnsupported is returned by every call in the public build:
// the external on/off device integration is not included.
var ErrOnoffFeederUnsupported = errors.New("external on/off device control is not supported in the public build")

// OnoffFeederControlAdapter is a placeholder that satisfies Adapter.
type OnoffFeederControlAdapter struct {
	cfg config.ControlAdapter
}

func NewOnoffFeederControlAdapter(cfg config.ControlAdapter) *OnoffFeederControlAdapter {
	return &OnoffFeederControlAdapter{cfg: cfg}
}

// Execute always fails so callers take their fail-safe path.
func (a *OnoffFeederControlAdapter) Execute(ctx context.Context, commandID, commandType string, params map[string]any) (map[string]any, error) {
	return nil, ErrOnoffFeederUnsupported
}
