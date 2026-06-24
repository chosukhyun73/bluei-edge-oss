package predictive

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	st "bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/wtg"
)

// Decision is the C-3p gate outcome.
type Decision string

const (
	DecisionAllow        Decision = "allow"
	DecisionConservative Decision = "conservative"
	DecisionBlock        Decision = "block"
)

// Gate implements feed_cycle.SafetyGate using D-7 headroom check (C-3p).
//
// The existing SafetyGate interface is Check(tankID string) (blocked bool, reason string).
// Gate maps that to a headroom-based decision: if current NH3 load exceeds the WTG
// capacity threshold, the cycle is blocked.
//
// D-6 per-cycle waste estimation is available via CheckWithFeed for use in API endpoints.
type Gate struct {
	store st.Store
	wtgs  []*wtg.Group // WTG registry loaded at startup
	cfg   config.PredictiveSafetyConfig
	log   *slog.Logger
}

// NewGate creates a new C-3p predictive safety gate.
func NewGate(store st.Store, groups []*wtg.Group, cfg config.PredictiveSafetyConfig) *Gate {
	return &Gate{
		store: store,
		wtgs:  groups,
		cfg:   cfg,
		log:   slog.With("component", "predictive_gate"),
	}
}

// Check implements feed_cycle.SafetyGate.
// Returns blocked=true only if WTG NH3 load is at or above rated capacity.
// Fail-open: missing WTG or no capacity config → always allow.
func (g *Gate) Check(tankID string) (blocked bool, reason string) {
	if !g.cfg.Enabled {
		return false, ""
	}

	group := g.findWTGForTank(tankID)
	if group == nil {
		// Tank has no WTG (marine cage case) → allow
		return false, ""
	}
	if group.Capacity.NH3ProcessingKgPerH == 0 {
		return false, ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	headroom, err := ComputeHeadroom(ctx, g.store, group)
	if err != nil {
		g.log.Warn("headroom compute failed; failing open", "wtg_id", group.WTGID, "error", err)
		return false, ""
	}

	cautionRatio := g.cfg.NH3CautionRatio
	if cautionRatio == 0 {
		cautionRatio = 0.7
	}

	threshold := headroom.MaxProcessingKgPerH
	caution := threshold * cautionRatio
	load := headroom.ActiveLoadKgPerH

	if load >= threshold {
		reason := fmt.Sprintf("nh3_threshold: load=%.4f kg/h >= capacity=%.4f kg/h", load, threshold)
		g.log.Warn("predictive gate: BLOCK", "tank_id", tankID, "wtg_id", group.WTGID, "reason", reason)
		g.insertBlock(tankID, group.WTGID, "nh3_threshold", load, threshold)
		return true, reason
	}

	if load >= caution {
		g.log.Info("predictive gate: CONSERVATIVE", "tank_id", tankID, "wtg_id", group.WTGID,
			"load_kg_per_h", load, "caution_threshold", caution)
		// Conservative: log + flag only (Phase 4 minimal — worker continues)
	}

	return false, ""
}

// CheckWithFeed runs a full D-6 + D-7 check given a planned feed amount.
// Used by the API endpoint; not called by the feed_cycle worker directly.
func (g *Gate) CheckWithFeed(ctx context.Context, tankID string, plannedFeedG float64) (Decision, string, error) {
	if !g.cfg.Enabled {
		return DecisionAllow, "predictive safety disabled", nil
	}

	group := g.findWTGForTank(tankID)
	if group == nil {
		return DecisionAllow, "no_wtg", nil
	}
	if group.Capacity.NH3ProcessingKgPerH == 0 {
		return DecisionAllow, "no_capacity_config", nil
	}

	headroom, err := ComputeHeadroom(ctx, g.store, group)
	if err != nil {
		return DecisionAllow, "headroom_error", err
	}

	cautionRatio := g.cfg.NH3CautionRatio
	if cautionRatio == 0 {
		cautionRatio = 0.7
	}

	threshold := headroom.MaxProcessingKgPerH
	load := headroom.ActiveLoadKgPerH

	switch {
	case load >= threshold:
		reason := fmt.Sprintf("load=%.4f kg/h >= capacity=%.4f kg/h", load, threshold)
		g.insertBlock(tankID, group.WTGID, "nh3_threshold", load, threshold)
		return DecisionBlock, reason, nil
	case load >= threshold*cautionRatio:
		reason := fmt.Sprintf("load=%.4f kg/h in caution zone (%.0f%% of %.4f kg/h)", load, cautionRatio*100, threshold)
		return DecisionConservative, reason, nil
	default:
		return DecisionAllow, "ok", nil
	}
}

func (g *Gate) findWTGForTank(tankID string) *wtg.Group {
	for _, group := range g.wtgs {
		for _, tid := range group.TankIDs {
			if tid == tankID {
				return group
			}
		}
	}
	return nil
}

func (g *Gate) insertBlock(tankID, wtgID, reason string, predicted, threshold float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	now := common.NowUTC()
	block := &st.PredictiveBlock{
		BlockID:        common.NewID("pblock"),
		WTGID:          wtgID,
		TankID:         tankID,
		Reason:         reason,
		PredictedValue: predicted,
		ThresholdValue: threshold,
		ForecastAt:     now,
		BlockedAt:      now,
	}
	if err := g.store.InsertPredictiveBlock(ctx, block); err != nil {
		g.log.Warn("predictive block insert failed", "error", err)
	}
}
