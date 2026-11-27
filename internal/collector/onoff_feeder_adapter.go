package collector

import (
	"context"
	"log/slog"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
)

// OnoffFeederAdapter is a placeholder: the external device integration is not
// included in the public build, so this adapter collects nothing.
type OnoffFeederAdapter struct {
	cfg config.CollectorAdapter
}

func NewOnoffFeederAdapter(app *runtime.App, cfg config.CollectorAdapter, devices *config.DevicesConfig) *OnoffFeederAdapter {
	return &OnoffFeederAdapter{cfg: cfg}
}

func (a *OnoffFeederAdapter) ID() string { return a.cfg.ID }

func (a *OnoffFeederAdapter) Type() string { return a.cfg.Type }

// Run logs once and returns.
func (a *OnoffFeederAdapter) Run(ctx context.Context) {
	slog.Warn("collector adapter type is not supported in the public build", "id", a.cfg.ID, "type", a.cfg.Type)
}
