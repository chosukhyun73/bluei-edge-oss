package biomass

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

// newHistoryTestStore — D-5 migration 포함.
func newHistoryTestStore(t *testing.T) storage.Store {
	t.Helper()
	migDir := findMigDirFromBiomass(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "hist_test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, name := range []string{
		"001_init.sql",
		"002_autonomous_mode.sql",
		"003_lifecycle.sql",
		"004_sampling.sql",
		"005_fcr_calibration.sql",
		"007_weight_history.sql",
		"030_traceability_lifecycle.sql",
	} {
		if err := storage.Migrate(st, filepath.Join(migDir, name)); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

func TestSnapshotSkipsWhenNoLifecycle(t *testing.T) {
	st := newHistoryTestStore(t)
	ok, err := SnapshotForTank(context.Background(), st, "tank_none", time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false when no lifecycle exists")
	}

	// upsert が呼ばれていないこと → ListTankWeightHistory で確認
	snaps, err := st.ListTankWeightHistory(context.Background(), "tank_none", 30)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("expected 0 snapshots, got %d", len(snaps))
	}
}

func TestSnapshotUpsertsActiveTank(t *testing.T) {
	st := newHistoryTestStore(t)
	ctx := context.Background()

	seedLifecycle(t, st, "tank_h1", "stocking_h1",
		time.Now().UTC().Add(-10*24*time.Hour), 50.0, 1000)

	now := time.Now().UTC()
	ok, err := SnapshotForTank(ctx, st, "tank_h1", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for active lifecycle")
	}

	snaps, err := st.ListTankWeightHistory(ctx, "tank_h1", 30)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	s := snaps[0]
	if s.TankID != "tank_h1" {
		t.Errorf("tank_id: got %q", s.TankID)
	}
	if s.EstimatedAvgWeightG < 50.0 {
		t.Errorf("estimated weight should be >= anchor weight (50g), got %v", s.EstimatedAvgWeightG)
	}
	if s.AnchorSource != "stocking" {
		t.Errorf("anchor_source: got %q, want stocking", s.AnchorSource)
	}
	if s.Quality == "" {
		t.Error("quality should not be empty")
	}
}

func TestSnapshotUsesSeoulDate(t *testing.T) {
	st := newHistoryTestStore(t)
	ctx := context.Background()

	seedLifecycle(t, st, "tank_tz", "stocking_tz",
		time.Now().UTC().Add(-5*24*time.Hour), 80.0, 500)

	// UTC 2026-05-10 15:30 == Asia/Seoul 2026-05-11 00:30 (다음날)
	utcMidnightish := time.Date(2026, 5, 10, 15, 30, 0, 0, time.UTC)

	ok, err := SnapshotForTank(ctx, st, "tank_tz", utcMidnightish)
	if err != nil {
		t.Fatalf("SnapshotForTank: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	snaps, err := st.ListTankWeightHistory(ctx, "tank_tz", 365)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("expected at least 1 snapshot")
	}

	loc, _ := time.LoadLocation("Asia/Seoul")
	wantDate := utcMidnightish.In(loc).Format("2006-01-02")
	if snaps[0].SnapshotDate != wantDate {
		t.Errorf("snapshot_date: got %q, want %q (Seoul local date)", snaps[0].SnapshotDate, wantDate)
	}
}
