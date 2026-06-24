package predictive

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	st "bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/wtg"
)

// --- mock store for gate tests -----------------------------------------------

type mockGateStore struct {
	st.Store // embed to satisfy interface; unused methods panic if called
	cycles   []*st.FeedCycle
	blocks   []*st.PredictiveBlock
}

func (m *mockGateStore) ListRecentFeedCycles(_ context.Context, tankID string, limit int) ([]*st.FeedCycle, error) {
	var out []*st.FeedCycle
	for _, c := range m.cycles {
		if c.TankID == tankID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *mockGateStore) InsertPredictiveBlock(_ context.Context, b *st.PredictiveBlock) error {
	m.blocks = append(m.blocks, b)
	return nil
}

// --- helpers -----------------------------------------------------------------

func newTestGate(store *mockGateStore, groups []*wtg.Group, cautionRatio float64) *Gate {
	return NewGate(store, groups, config.PredictiveSafetyConfig{
		Enabled:         true,
		NH3CautionRatio: cautionRatio,
	})
}

// singleTankWTG creates a WTG with one tank and the given NH3 capacity.
func singleTankWTG(wtgID, tankID string, nh3CapKgPerH float64) *wtg.Group {
	return &wtg.Group{
		WTGID:   wtgID,
		TankIDs: []string{tankID},
		Capacity: wtg.Capacity{
			NH3ProcessingKgPerH: nh3CapKgPerH,
		},
	}
}

// addCycle appends a cycle to the mock store started within the last 5 min.
func addCycle(store *mockGateStore, tankID string, totalAmountG float64) {
	store.cycles = append(store.cycles, &st.FeedCycle{
		CycleID:      "test_cycle",
		TankID:       tankID,
		TotalAmountG: totalAmountG,
		StartedAt:    time.Now().UTC().Add(-5 * time.Minute),
	})
}

// --- tests -------------------------------------------------------------------

// TestGate_Allow verifies that a load well below threshold returns Allow and no block.
//
// WTG capacity = 1.0 kg/h NH3.
// Recent cycles: 100g feed → load = 0.1kg × 0.0045 / 1h ≈ 0.00045 kg/h
// 0.00045 < 0.7 × 1.0 = 0.7 → Allow.
func TestGate_Allow(t *testing.T) {
	store := &mockGateStore{}
	addCycle(store, "tank_01", 100) // 100g dispensed
	gate := newTestGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 1.0)}, 0.7)

	blocked, reason := gate.Check("tank_01")
	if blocked {
		t.Errorf("expected Allow, got blocked: %q", reason)
	}
	if len(store.blocks) != 0 {
		t.Errorf("expected no blocks inserted, got %d", len(store.blocks))
	}
}

// TestGate_Conservative verifies Conservative zone: load ≥ 70% but < 100% capacity.
//
// WTG capacity = 0.00001 kg/h (artificially tiny to force caution zone).
// Load from 100g cycle = 0.0045 kg/h (>> caution but < capacity? No — let's set it up properly).
//
// We want: caution ≤ load < capacity.
// capacity = 0.006 kg/h, caution = 0.7 × 0.006 = 0.0042 kg/h.
// cycle 100g → load ≈ 0.00045 kg/h (still below caution).
//
// To hit caution: need load ≥ 0.0042.
// cycle 1000g → load = 1.0kg × 0.0045 / 1h = 0.0045 kg/h. 0.0042 ≤ 0.0045 < 0.006. ✓
func TestGate_Conservative(t *testing.T) {
	store := &mockGateStore{}
	addCycle(store, "tank_01", 1000) // 1000g dispensed
	gate := newTestGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 0.006)}, 0.7)

	blocked, reason := gate.Check("tank_01")
	// Conservative doesn't block — worker continues but gate logs a warning.
	if blocked {
		t.Errorf("Conservative should NOT block, but got blocked: %q", reason)
	}
	if len(store.blocks) != 0 {
		t.Errorf("Conservative should not insert a block row, got %d", len(store.blocks))
	}
}

// TestGate_Block verifies Block decision when load ≥ capacity.
//
// capacity = 0.003 kg/h.
// cycle 1000g → load ≈ 0.0045 kg/h ≥ 0.003 → Block.
func TestGate_Block(t *testing.T) {
	store := &mockGateStore{}
	addCycle(store, "tank_01", 1000)
	gate := newTestGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 0.003)}, 0.7)

	blocked, reason := gate.Check("tank_01")
	if !blocked {
		t.Errorf("expected Block, got Allow (reason=%q)", reason)
	}
	if len(store.blocks) != 1 {
		t.Errorf("expected 1 block row inserted, got %d", len(store.blocks))
	}
	b := store.blocks[0]
	if b.TankID != "tank_01" {
		t.Errorf("block row tank_id = %q; want tank_01", b.TankID)
	}
	if b.WTGID != "wtg_01" {
		t.Errorf("block row wtg_id = %q; want wtg_01", b.WTGID)
	}
}

// TestGate_NoWTG verifies fail-open when tank has no WTG (marine cage case).
func TestGate_NoWTG(t *testing.T) {
	store := &mockGateStore{}
	gate := newTestGate(store, []*wtg.Group{}, 0.7)

	blocked, _ := gate.Check("marine_cage_01")
	if blocked {
		t.Error("expected Allow for tank with no WTG")
	}
}

// TestGate_NoCapacityConfig verifies fail-open when WTG has zero capacity configured.
func TestGate_NoCapacityConfig(t *testing.T) {
	store := &mockGateStore{}
	gate := newTestGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 0)}, 0.7)

	blocked, _ := gate.Check("tank_01")
	if blocked {
		t.Error("expected Allow when WTG has no capacity configured")
	}
}

// TestGate_Disabled verifies that a disabled gate always allows.
func TestGate_Disabled(t *testing.T) {
	store := &mockGateStore{}
	addCycle(store, "tank_01", 999999) // extreme load
	gate := NewGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 0.001)},
		config.PredictiveSafetyConfig{Enabled: false})

	blocked, _ := gate.Check("tank_01")
	if blocked {
		t.Error("disabled gate should always allow")
	}
}

// TestCheckWithFeed_Allow exercises the API-level D-6+D-7 check.
func TestCheckWithFeed_Allow(t *testing.T) {
	store := &mockGateStore{}
	gate := newTestGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 1.0)}, 0.7)

	decision, reason, err := gate.CheckWithFeed(context.Background(), "tank_01", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionAllow {
		t.Errorf("expected Allow, got %q (%s)", decision, reason)
	}
}

// TestCheckWithFeed_Block exercises Block path via API check.
func TestCheckWithFeed_Block(t *testing.T) {
	store := &mockGateStore{}
	addCycle(store, "tank_01", 1000) // load ≈ 0.0045 kg/h
	gate := newTestGate(store, []*wtg.Group{singleTankWTG("wtg_01", "tank_01", 0.003)}, 0.7)

	decision, _, err := gate.CheckWithFeed(context.Background(), "tank_01", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != DecisionBlock {
		t.Errorf("expected Block, got %q", decision)
	}
}
