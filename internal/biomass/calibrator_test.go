package biomass

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// ── 공통 헬퍼 ────────────────────────────────────────────────────────────────

func newCalibratorTestStore(t *testing.T) storage.Store {
	t.Helper()
	migDir := findMigDirFromBiomass(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cal_test.db")
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
		"030_traceability_lifecycle.sql",
	} {
		if err := storage.Migrate(st, filepath.Join(migDir, name)); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

func newCalibratorTestApp(t *testing.T, st storage.Store) *runtime.App {
	t.Helper()
	return runtime.NewApp(&config.Config{}, st)
}

// seedLifecycle — 입식 lifecycle 직접 주입 (stocked_at 자유 제어).
func seedLifecycle(t *testing.T, st storage.Store, tankID, stockingID string, stokedAt time.Time, initialWeightG float64, count int) {
	t.Helper()
	lc := &storage.TankLifecycle{
		TankID:            tankID,
		ActiveStockingID:  stockingID,
		Species:           "참돔",
		GrowthStage:       "juvenile",
		InitialCount:      count,
		InitialAvgWeightG: initialWeightG,
		StockedAt:         stokedAt,
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.UpsertTankLifecycle(context.Background(), lc); err != nil {
		t.Fatalf("seedLifecycle: %v", err)
	}
}

// seedSampling — sampling projection 직접 주입 (sampled_at 자유 제어).
func seedSampling(t *testing.T, st storage.Store, tankID, samplingID, stockingID string, sampledAt time.Time, avgWeightG float64) {
	t.Helper()
	ts := &storage.TankSampling{
		TankID:           tankID,
		LatestSamplingID: samplingID,
		StockingID:       stockingID,
		SampledCount:     30,
		AvgWeightG:       avgWeightG,
		SampledAt:        sampledAt,
		RecordedBy:       "op1",
		UpdatedAt:        time.Now().UTC(),
	}
	if err := st.UpsertTankSampling(context.Background(), ts); err != nil {
		t.Fatalf("seedSampling: %v", err)
	}
}

// seedFeedingEvent — feeding 이벤트를 DB 에 직접 INSERT (fed_at 타임스탬프 제어).
func seedFeedingEvent(t *testing.T, st storage.Store, tankID string, feedAmountG float64, fedAt time.Time) {
	t.Helper()
	app := newCalibratorTestApp(t, st)
	p := events.FeedingRecordedPayload{
		FeedingID:   fmt.Sprintf("feed_%d", fedAt.UnixNano()),
		TankID:      tankID,
		Source:      events.FeedingSourceManual,
		FeedAmountG: feedAmountG,
		FedAt:       fedAt.Format(time.RFC3339Nano),
		Quality:     events.QualityOK,
	}
	b, _ := json.Marshal(p)
	evt := &storage.Event{
		EventID:       p.FeedingID,
		EventType:     events.EventFeedingRecorded,
		SchemaVersion: "1.0",
		SiteID:        "site_test",
		EdgeID:        "edge_test",
		RecordedAt:    fedAt, // RecordedAt 도 fed_at 으로 맞춤 (Since 필터 기준)
		SourceModule:  "test",
		PayloadJSON:   string(b),
		EventJSON:     string(b),
	}
	if _, err := st.AppendEvent(context.Background(), evt); err != nil {
		t.Fatalf("seedFeedingEvent: %v", err)
	}
	_ = app
}

// ── 단위 테스트 ───────────────────────────────────────────────────────────────

func TestCalibrateFromSamplingNoLifecycle(t *testing.T) {
	st := newCalibratorTestStore(t)
	res, err := CalibrateFromSampling(context.Background(), st, "no_such_tank", "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false")
	}
	if res.Reason != "no_active_lifecycle" {
		t.Errorf("expected reason=no_active_lifecycle, got %q", res.Reason)
	}
}

func TestCalibrateFromSamplingNoSampling(t *testing.T) {
	st := newCalibratorTestStore(t)
	seedLifecycle(t, st, "tank_01", "stocking_01", time.Now().UTC().Add(-30*24*time.Hour), 50.0, 1000)

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false")
	}
	if res.Reason != "no_sampling" {
		t.Errorf("expected reason=no_sampling, got %q", res.Reason)
	}
}

func TestCalibrateFromSamplingPeriodTooShort(t *testing.T) {
	st := newCalibratorTestStore(t)
	// 입식 5일 전
	stockedAt := time.Now().UTC().Add(-5 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	seedSampling(t, st, "tank_01", "sampling_01", "stocking_01", sampledAt, 52.0)

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false")
	}
	if res.Reason != "period_too_short" {
		t.Errorf("expected reason=period_too_short, got %q", res.Reason)
	}
}

func TestCalibrateFromSamplingNonPositiveGrowth(t *testing.T) {
	st := newCalibratorTestStore(t)
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	// sampling avg ≤ initial: 체중 감소 또는 동일 → non_positive_growth
	seedSampling(t, st, "tank_01", "sampling_01", "stocking_01", sampledAt, 50.0)

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false")
	}
	if res.Reason != "non_positive_growth" {
		t.Errorf("expected reason=non_positive_growth, got %q", res.Reason)
	}
}

func TestCalibrateFromSamplingNoFeeding(t *testing.T) {
	st := newCalibratorTestStore(t)
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	seedSampling(t, st, "tank_01", "sampling_01", "stocking_01", sampledAt, 58.0)
	// feeding 이벤트 없음

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false")
	}
	if res.Reason != "no_feeding_in_period" {
		t.Errorf("expected reason=no_feeding_in_period, got %q", res.Reason)
	}
}

func TestCalibrateFromSamplingFCROutOfRange(t *testing.T) {
	st := newCalibratorTestStore(t)
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	seedSampling(t, st, "tank_01", "sampling_01", "stocking_01", sampledAt, 58.0)
	// ΔBiomass = (58-50)*1000 = 8000g, feed = 100g → FCR = 100/8000 = 0.0125 < 0.5
	seedFeedingEvent(t, st, "tank_01", 100.0, stockedAt.Add(24*time.Hour))

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false")
	}
	if len(res.Reason) == 0 || res.Reason[:14] != "fcr_out_of_ran" {
		t.Errorf("expected reason=fcr_out_of_range..., got %q", res.Reason)
	}
}

func TestCalibrateFromSamplingHappyPath(t *testing.T) {
	st := newCalibratorTestStore(t)
	// stocking 30일 전, W₀=50, N=1000
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	// sampling avg=58 → ΔBiomass=(58-50)*1000=8000g
	seedSampling(t, st, "tank_01", "sampling_01", "stocking_01", sampledAt, 58.0)
	// feed=10000g → FCR = 10000/8000 = 1.25
	seedFeedingEvent(t, st, "tank_01", 10000.0, stockedAt.Add(15*24*time.Hour))

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Performed {
		t.Fatalf("expected Performed=true, reason=%q", res.Reason)
	}

	// ObservedFCR ≈ 1.25
	const wantFCR = 10000.0 / 8000.0
	if res.ObservedFCR < 1.24 || res.ObservedFCR > 1.26 {
		t.Errorf("ObservedFCR: got %v, want ~%.4f", res.ObservedFCR, wantFCR)
	}
	// DefaultFCR = 1.4 (참돔/juvenile)
	if res.DefaultFCR != 1.4 {
		t.Errorf("DefaultFCR: got %v, want 1.4", res.DefaultFCR)
	}
	// DeviationPct ≈ (1.25-1.4)/1.4*100 ≈ -10.71%
	if res.DeviationPct > -10.0 || res.DeviationPct < -12.0 {
		t.Errorf("DeviationPct: got %v, want ~-10.7%%", res.DeviationPct)
	}
	if res.DeltaBiomassG != 8000.0 {
		t.Errorf("DeltaBiomassG: got %v, want 8000", res.DeltaBiomassG)
	}
	if res.CumulativeFeedG != 10000.0 {
		t.Errorf("CumulativeFeedG: got %v, want 10000", res.CumulativeFeedG)
	}
}

func TestCalibrateFromSamplingLargeDeviationNote(t *testing.T) {
	st := newCalibratorTestStore(t)
	// DeviationPct > 30%: feed 를 매우 많이 줘서 FCR 을 높임
	// ΔBiomass = (58-50)*1000 = 8000g
	// feed = 35000g → FCR = 35000/8000 = 4.375 → 범위 초과 (> 3.0) → 걸릴 것
	// 대신 FCR ≈ 2.5 (>1.4 + 30% = 1.82) 로:
	// feed = 2.5 * 8000 = 20000g
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	seedSampling(t, st, "tank_01", "sampling_01", "stocking_01", sampledAt, 58.0)
	// FCR = 20000/8000 = 2.5 → default 1.4 대비 편차 = (2.5-1.4)/1.4*100 ≈ +78.6% > 30%
	seedFeedingEvent(t, st, "tank_01", 20000.0, stockedAt.Add(15*24*time.Hour))

	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Performed {
		t.Fatalf("expected Performed=true, reason=%q", res.Reason)
	}

	found := false
	const wantPrefix = "정상 편차 범위"
	for _, n := range res.Notes {
		if len(n) >= len(wantPrefix) && n[:len(wantPrefix)] == wantPrefix {
			found = true
		}
	}
	if !found {
		t.Errorf("expected deviation warning note, got notes: %v", res.Notes)
	}
}

func TestCalibrateFromSamplingSamplingIDMismatch(t *testing.T) {
	st := newCalibratorTestStore(t)
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	sampledAt := time.Now().UTC().Add(-time.Hour)
	seedLifecycle(t, st, "tank_01", "stocking_01", stockedAt, 50.0, 1000)
	seedSampling(t, st, "tank_01", "sampling_latest", "stocking_01", sampledAt, 58.0)

	// 오래된 sampling_id 로 호출 — latest 와 다름
	res, err := CalibrateFromSampling(context.Background(), st, "tank_01", "sampling_old")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Performed {
		t.Error("expected Performed=false for sampling_id mismatch")
	}
	if res.Reason != "sampling_id_mismatch" {
		t.Errorf("expected reason=sampling_id_mismatch, got %q", res.Reason)
	}
}
