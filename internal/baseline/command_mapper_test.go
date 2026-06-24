package baseline_test

import (
	"testing"

	"bluei.kr/edge/internal/baseline"
)

func TestPlanCommandFeedingHappyPath(t *testing.T) {
	plan := baseline.PlanCommand("feeding", map[string]any{
		"feeder_id":     "feeder_01",
		"feed_amount_g": float64(200),
	})
	if !plan.SupportedKind {
		t.Fatalf("expected SupportedKind=true, got false (reason: %s)", plan.Reason)
	}
	if plan.DeviceID != "feeder_01" {
		t.Errorf("expected DeviceID=feeder_01, got %q", plan.DeviceID)
	}
	if plan.CommandType != "dispense_feed" {
		t.Errorf("expected CommandType=dispense_feed, got %q", plan.CommandType)
	}
	amt, ok := plan.CommandBody["feed_amount_g"].(float64)
	if !ok || amt != 200 {
		t.Errorf("expected feed_amount_g=200, got %v", plan.CommandBody["feed_amount_g"])
	}
}

func TestPlanCommandFeedingMissingFeederID(t *testing.T) {
	plan := baseline.PlanCommand("feeding", map[string]any{
		"feed_amount_g": float64(100),
		// feeder_id 없음
	})
	if plan.SupportedKind {
		t.Errorf("expected SupportedKind=false when feeder_id missing")
	}
	if plan.Reason == "" {
		t.Errorf("expected non-empty Reason")
	}
}

func TestPlanCommandFeedingMissingAmount(t *testing.T) {
	plan := baseline.PlanCommand("feeding", map[string]any{
		"feeder_id": "feeder_01",
		// feed_amount_g 없음
	})
	if plan.SupportedKind {
		t.Errorf("expected SupportedKind=false when feed_amount_g missing")
	}
	if plan.Reason == "" {
		t.Errorf("expected non-empty Reason")
	}
}

func TestPlanCommandUnsupportedKind(t *testing.T) {
	for _, kind := range []string{"oxygen_supply", "water_exchange", "pump_adjust", "monitoring"} {
		plan := baseline.PlanCommand(kind, map[string]any{})
		if plan.SupportedKind {
			t.Errorf("kind=%s: expected SupportedKind=false", kind)
		}
		if plan.Reason == "" {
			t.Errorf("kind=%s: expected non-empty Reason", kind)
		}
		// 사유 메시지에 kind 이름이 포함되어야 함
		if plan.Reason != "control wiring pending for kind="+kind {
			t.Errorf("kind=%s: unexpected reason %q", kind, plan.Reason)
		}
	}
}
