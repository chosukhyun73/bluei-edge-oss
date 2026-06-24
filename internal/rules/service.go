package rules

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

const cursorKey = "rules_cursor_sequence"

// Service is the rules worker: it reads events from the event log cursor
// and evaluates configured rules.
type Service struct {
	app    *runtime.App
	cfg    *config.RulesConfig
	store  storage.Store
	cancel context.CancelFunc
}

func NewService(app *runtime.App, cfg *config.RulesConfig, store storage.Store) *Service {
	return &Service{app: app, cfg: cfg, store: store}
}

func (s *Service) Name() string { return "rules" }

func (s *Service) Start(ctx context.Context) error {
	if s.cfg == nil || len(s.cfg.Rules) == 0 {
		s.app.Health.Set("rules", "disabled", "no rules configured")
		return nil
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.app.Health.Set("rules", "starting", "")
	go s.loop(ctx)
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	s.app.Health.Set("rules", "ok", "")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.evaluate(ctx); err != nil {
				slog.Error("rules evaluation error", "error", err)
				s.app.Health.Set("rules", "degraded", err.Error())
			} else {
				s.app.Health.Set("rules", "ok", "")
				s.app.Health.Touch("rules")
			}
		}
	}
}

func (s *Service) evaluate(ctx context.Context) error {
	cursorStr, _, err := s.store.KVGet(ctx, cursorKey)
	if err != nil {
		return err
	}
	var cursor int64
	if cursorStr != "" {
		cursor, _ = strconv.ParseInt(cursorStr, 10, 64)
	}

	events, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "sensor.reading.recorded",
		AfterSeq:  cursor,
		Limit:     500,
	})
	if err != nil {
		return err
	}
	for _, rule := range s.cfg.Rules {
		if !rule.Enabled {
			continue
		}
		switch rule.Type {
		case "threshold":
			if err := evaluateThreshold(ctx, s.app, s.store, rule, events); err != nil {
				slog.Warn("threshold rule error", "rule_id", rule.RuleID, "error", err)
			}
		case "missing_data":
			if err := evaluateMissingData(ctx, s.app, s.store, rule); err != nil {
				slog.Warn("missing_data rule error", "rule_id", rule.RuleID, "error", err)
			}
		}
	}

	// Advance cursor to the highest sequence seen. Missing-data rules still run
	// when no new readings arrive, but the event cursor only advances with input.
	var maxSeq int64
	for _, e := range events {
		if e.Sequence > maxSeq {
			maxSeq = e.Sequence
		}
	}
	if maxSeq > cursor {
		if err := s.store.KVSet(ctx, cursorKey, strconv.FormatInt(maxSeq, 10)); err != nil {
			return err
		}
	}
	return nil
}
