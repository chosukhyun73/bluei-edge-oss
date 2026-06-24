package collector

import (
	"context"
	"log/slog"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
)

// Service manages all collector adapters.
type Service struct {
	app      *runtime.App
	cfg      *config.CollectorConfig
	devices  *config.DevicesConfig
	adapters []Adapter
	cancel   context.CancelFunc
}

func NewService(app *runtime.App, cfg *config.CollectorConfig, devices *config.DevicesConfig) *Service {
	return &Service{app: app, cfg: cfg, devices: devices}
}

func (s *Service) Name() string { return "collector" }

func (s *Service) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.app.Health.Set("collector", "disabled", "collector disabled by config")
		return nil
	}

	// Long-running adapter goroutine 은 startup ctx 에 묶이면 안 됨 — Stop() 으로만 종료.
	ctx, s.cancel = context.WithCancel(context.Background())
	s.app.Health.Set("collector", "starting", "")

	for _, adCfg := range s.cfg.Adapters {
		switch adCfg.Type {
		case "mock_sensor":
			adapter := NewMockAdapter(s.app, adCfg, s.devices)
			s.startAdapter(ctx, adapter)
		case "mock_feeder":
			adapter := NewMockFeederAdapter(s.app, adCfg, s.devices)
			s.startAdapter(ctx, adapter)
		default:
			slog.Warn("unknown collector adapter type", "type", adCfg.Type, "id", adCfg.ID)
		}
	}

	s.app.Health.Set("collector", "ok", "")
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	s.app.Health.Set("collector", "stopping", "")
	return nil
}

func (s *Service) startAdapter(ctx context.Context, adapter Adapter) {
	s.adapters = append(s.adapters, adapter)
	go adapter.Run(ctx)
	slog.Info("collector adapter started", "id", adapter.ID(), "type", adapter.Type())
}
