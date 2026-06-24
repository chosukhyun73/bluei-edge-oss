package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

// findMigDirForFC — feed_cycle_intent_test 전용 helper.
// 호출 위치(test working dir)와 무관하게 migrations/ 디렉터리 절대 경로를 반환한다.
func findMigDirForFC(t *testing.T) string {
	t.Helper()
	p, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		c := filepath.Join(p, "migrations")
		if _, err := os.Stat(filepath.Join(c, "001_init.sql")); err == nil {
			return c
		}
		p = filepath.Dir(p)
	}
	t.Fatal("migrations/ not found from any parent")
	return ""
}

// openFeedCycleTestStore — 실제 migration 015 (feed_cycles 테이블) + 018 (priority) +
// 019 (intent_id) 까지 적용한 store 를 반환한다.
func openFeedCycleTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fc_test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	migDir := findMigDirForFC(t)
	for _, name := range []string{
		"001_init.sql", "015_predictive_events.sql",
		"017_arbiter_decisions.sql", "018_arbiter_preemption.sql",
		"019_feed_cycle_intent.sql",
		"026_feed_cycle_motor_outputs.sql",
		"028_feed_cycle_load_cell.sql",
	} {
		p := filepath.Join(migDir, name)
		if err := storage.Migrate(st, p); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

// TestFeedCycleIntentRoundTrip — C-1: intent_id 가 INSERT → SELECT round-trip.
// 비어 있는 intent_id 와 채워진 intent_id 양쪽을 모두 검증한다.
func TestFeedCycleIntentRoundTrip(t *testing.T) {
	store := openFeedCycleTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. intent_id 채워진 cycle
	c1 := &storage.FeedCycle{
		CycleID:       "cycle_with_intent",
		TankID:        "tank_01",
		ControllerID:  "ctrl_01",
		Mode:          "adaptive",
		TargetAmountG: 50.0,
		StartedAt:     now,
		Priority:      "manual_override",
		IntentID:      "intent_op_001",
	}
	if err := store.InsertFeedCycle(ctx, c1); err != nil {
		t.Fatalf("InsertFeedCycle with intent: %v", err)
	}

	got1, err := store.GetFeedCycle(ctx, "cycle_with_intent")
	if err != nil {
		t.Fatalf("GetFeedCycle: %v", err)
	}
	if got1 == nil {
		t.Fatal("expected cycle row, got nil")
	}
	if got1.IntentID != "intent_op_001" {
		t.Errorf("IntentID round-trip: got %q, want intent_op_001", got1.IntentID)
	}

	// 2. intent_id 없는 cycle — 기존 동작 보존 (회귀 검증)
	c2 := &storage.FeedCycle{
		CycleID:      "cycle_no_intent",
		TankID:       "tank_02",
		ControllerID: "ctrl_02",
		Mode:         "fixed",
		StartedAt:    now.Add(time.Second),
		Priority:     "ai_autonomous",
		IntentID:     "",
	}
	if err := store.InsertFeedCycle(ctx, c2); err != nil {
		t.Fatalf("InsertFeedCycle without intent: %v", err)
	}
	got2, err := store.GetFeedCycle(ctx, "cycle_no_intent")
	if err != nil {
		t.Fatalf("GetFeedCycle no-intent: %v", err)
	}
	if got2 == nil {
		t.Fatal("expected no-intent cycle row")
	}
	if got2.IntentID != "" {
		t.Errorf("IntentID: expected empty for no-intent cycle, got %q", got2.IntentID)
	}

	// 3. List 응답에도 round-trip
	cycles, err := store.ListRecentFeedCycles(ctx, "tank_01", 10)
	if err != nil {
		t.Fatalf("ListRecentFeedCycles: %v", err)
	}
	if len(cycles) != 1 || cycles[0].IntentID != "intent_op_001" {
		t.Fatalf("ListRecentFeedCycles: intent_id missing, got %#v", cycles)
	}
}
