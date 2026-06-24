package biomass

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// ── pure-function tests (DB 없음) ────────────────────────────────────────────

func TestProjectFromInputsHappyPath(t *testing.T) {
	// W₀=50g, N=1000, FCR=1.4(참돔/juvenile), feed=20kg(20000g)
	// ΔBiomass=20000/1.4≈14285.7g, ΔW≈14.286g, est≈64.286g
	in := ProjectionInputs{
		Species:           "참돔",
		GrowthStage:       "juvenile",
		Now:               time.Now().UTC(),
		AnchorWeightG:     50.0,
		AnchorN:           1000,
		AnchorAt:          time.Now().UTC().Add(-48 * time.Hour),
		AnchorSource:      "stocking",
		CumulativeFeedG:   20000.0,
		DaysSinceSampling: -1,
	}
	res := ProjectFromInputs(in)

	if res.FCRKnown != true {
		t.Errorf("expected FCRKnown=true, got false")
	}
	if math.Abs(res.ExpectedFCR-1.4) > 0.001 {
		t.Errorf("expected FCR=1.4, got %v", res.ExpectedFCR)
	}
	expectedEst := 50.0 + (20000.0/1.4)/1000.0
	if math.Abs(res.EstimatedAvgWeightG-expectedEst) > 0.1 {
		t.Errorf("estimated weight: got %v, want ~%v", res.EstimatedAvgWeightG, expectedEst)
	}
	if res.Quality != "ok" {
		t.Errorf("expected quality=ok, got %q", res.Quality)
	}
}

func TestProjectFromInputsUsesSamplingAnchor(t *testing.T) {
	// sampling anchor 가 stocking 보다 우선됨을 입력값으로 검증
	sampledAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	in := ProjectionInputs{
		Species:           "연어",
		GrowthStage:       "growout",
		Now:               time.Now().UTC(),
		AnchorWeightG:     300.0, // 이미 sampling 값 (stocking W₀=50g 아님)
		AnchorN:           1000,
		AnchorAt:          sampledAt,
		AnchorSource:      "sampling",
		CumulativeFeedG:   5000.0,
		DaysSinceSampling: 10,
	}
	res := ProjectFromInputs(in)

	if res.AnchorSource != "sampling" {
		t.Errorf("expected AnchorSource=sampling, got %q", res.AnchorSource)
	}
	if res.AnchorWeightG != 300.0 {
		t.Errorf("expected AnchorWeightG=300, got %v", res.AnchorWeightG)
	}
	// est > 300 (급이가 있으므로)
	if res.EstimatedAvgWeightG <= 300.0 {
		t.Errorf("expected est > anchor, got %v", res.EstimatedAvgWeightG)
	}
	if res.Quality != "ok" {
		t.Errorf("expected quality=ok (DaysSinceSampling=10), got %q", res.Quality)
	}
}

func TestProjectFromInputsUnknownSpecies(t *testing.T) {
	in := ProjectionInputs{
		Species:           "미등록어종",
		GrowthStage:       "growout",
		Now:               time.Now().UTC(),
		AnchorWeightG:     100.0,
		AnchorN:           500,
		AnchorAt:          time.Now().UTC().Add(-48 * time.Hour),
		AnchorSource:      "stocking",
		CumulativeFeedG:   10000.0,
		DaysSinceSampling: -1,
	}
	res := ProjectFromInputs(in)

	if res.FCRKnown {
		t.Error("expected FCRKnown=false for unknown species")
	}
	if math.Abs(res.ExpectedFCR-DefaultFCR) > 0.001 {
		t.Errorf("expected DefaultFCR=1.5, got %v", res.ExpectedFCR)
	}
	// note 에 미스 메시지 포함
	found := false
	for _, n := range res.Notes {
		if n == "어종/단계 FCR 룩업 미스 — default 1.5 사용" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected lookup miss note, got: %v", res.Notes)
	}
}

func TestProjectFromInputsZeroAnchorN(t *testing.T) {
	in := ProjectionInputs{
		Species:     "참돔",
		GrowthStage: "growout",
		Now:         time.Now().UTC(),
		AnchorN:     0,
		AnchorAt:    time.Now().UTC().Add(-24 * time.Hour),
	}
	res := ProjectFromInputs(in)

	if res.Quality != "low_data" {
		t.Errorf("expected quality=low_data, got %q", res.Quality)
	}
}

func TestProjectFromInputsNoFeed(t *testing.T) {
	// CumulativeFeedG=0 → Estimated == Anchor
	in := ProjectionInputs{
		Species:           "광어",
		GrowthStage:       "growout",
		Now:               time.Now().UTC(),
		AnchorWeightG:     200.0,
		AnchorN:           800,
		AnchorAt:          time.Now().UTC().Add(-48 * time.Hour),
		AnchorSource:      "stocking",
		CumulativeFeedG:   0,
		DaysSinceSampling: -1,
	}
	res := ProjectFromInputs(in)

	if res.EstimatedAvgWeightG != 200.0 {
		t.Errorf("expected est=anchor=200g when no feed, got %v", res.EstimatedAvgWeightG)
	}
}

func TestProjectFromInputsClampsBelowAnchor(t *testing.T) {
	// CumulativeFeedG 음수는 실제 불가능하지만 방어 코드 확인
	// 음수 feed 를 넣으면 est < anchor → clamp to anchor
	in := ProjectionInputs{
		Species:           "우럭",
		GrowthStage:       "growout",
		Now:               time.Now().UTC(),
		AnchorWeightG:     150.0,
		AnchorN:           1000,
		AnchorAt:          time.Now().UTC().Add(-48 * time.Hour),
		AnchorSource:      "stocking",
		CumulativeFeedG:   -5000.0, // 비정상 음수
		DaysSinceSampling: -1,
	}
	res := ProjectFromInputs(in)

	if res.EstimatedAvgWeightG < in.AnchorWeightG {
		t.Errorf("clamp failed: est %v < anchor %v", res.EstimatedAvgWeightG, in.AnchorWeightG)
	}
}

func TestProjectFromInputsStaleSampling(t *testing.T) {
	in := ProjectionInputs{
		Species:           "농어",
		GrowthStage:       "growout",
		Now:               time.Now().UTC(),
		AnchorWeightG:     400.0,
		AnchorN:           600,
		AnchorAt:          time.Now().UTC().Add(-40 * 24 * time.Hour),
		AnchorSource:      "sampling",
		CumulativeFeedG:   50000.0,
		DaysSinceSampling: 40, // > 35
	}
	res := ProjectFromInputs(in)

	if res.Quality != "stale_sampling" {
		t.Errorf("expected quality=stale_sampling, got %q", res.Quality)
	}
}

// ── integration tests (SQLite) ───────────────────────────────────────────────

func findMigDirFromBiomass(t *testing.T) string {
	t.Helper()
	p, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(p, "migrations")
		if _, err := os.Stat(filepath.Join(candidate, "001_init.sql")); err == nil {
			return candidate
		}
		p = filepath.Dir(p)
	}
	t.Fatal("migrations/ not found")
	return ""
}

func newBiomassTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	migDir := findMigDirFromBiomass(t)
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

func TestLoadAndProjectNoLifecycle(t *testing.T) {
	st := newBiomassTestStore(t)
	res, ok, err := LoadAndProject(context.Background(), st, "tank_empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for empty store")
	}
	if res.Quality != "no_lifecycle" {
		t.Errorf("expected quality=no_lifecycle, got %q", res.Quality)
	}
}

func TestLoadAndProjectStockingOnly(t *testing.T) {
	st := newBiomassTestStore(t)
	ctx := context.Background()

	// 입식 projection 직접 upsert
	lc := &storage.TankLifecycle{
		TankID:            "tank_01",
		ActiveStockingID:  "stocking_test_01",
		Species:           "참돔",
		GrowthStage:       "juvenile",
		InitialCount:      1000,
		InitialAvgWeightG: 50.0,
		StockedAt:         time.Now().UTC().Add(-5 * 24 * time.Hour),
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("upsert lifecycle: %v", err)
	}

	// feeding event 주입
	app := newBiomassTestApp(t, st)
	payload := events.FeedingRecordedPayload{
		FeedingID:   "feed_01",
		TankID:      "tank_01",
		FeederID:    "feeder_01",
		Source:      "manual",
		FeedAmountG: 10000.0,
		FedAt:       time.Now().UTC().Add(-2 * 24 * time.Hour).Format(time.RFC3339Nano),
		Quality:     "ok",
	}
	_, err := app.AppendEvent(ctx, "test", "feeder", "feeder_01",
		events.EventFeedingRecorded, "feed_01", payload)
	if err != nil {
		t.Fatalf("append feeding event: %v", err)
	}

	res, ok, err := LoadAndProject(ctx, st, "tank_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.AnchorSource != "stocking" {
		t.Errorf("expected AnchorSource=stocking, got %q", res.AnchorSource)
	}
	if res.EstimatedAvgWeightG <= 50.0 {
		t.Errorf("expected est > 50g (W₀), got %v", res.EstimatedAvgWeightG)
	}
}

func TestLoadAndProjectWithSamplingOverride(t *testing.T) {
	st := newBiomassTestStore(t)
	ctx := context.Background()

	// 입식
	lc := &storage.TankLifecycle{
		TankID:            "tank_02",
		ActiveStockingID:  "stocking_test_02",
		Species:           "연어",
		GrowthStage:       "growout",
		InitialCount:      500,
		InitialAvgWeightG: 100.0,
		StockedAt:         time.Now().UTC().Add(-60 * 24 * time.Hour),
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("upsert lifecycle: %v", err)
	}

	// sampling projection upsert (30일 전)
	sampledAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	ts := &storage.TankSampling{
		TankID:           "tank_02",
		LatestSamplingID: "sampling_test_01",
		StockingID:       "stocking_test_02",
		SampledCount:     30,
		AvgWeightG:       250.0, // stocking W₀=100g 보다 큰 값
		SampledAt:        sampledAt,
		RecordedBy:       "op1",
		UpdatedAt:        time.Now().UTC(),
	}
	if err := st.UpsertTankSampling(ctx, ts); err != nil {
		t.Fatalf("upsert sampling: %v", err)
	}

	res, ok, err := LoadAndProject(ctx, st, "tank_02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.AnchorSource != "sampling" {
		t.Errorf("expected AnchorSource=sampling, got %q", res.AnchorSource)
	}
	if res.AnchorWeightG != 250.0 {
		t.Errorf("expected AnchorWeightG=250 (sampling), got %v", res.AnchorWeightG)
	}
}

// newBiomassTestApp — AppendEvent 가 필요한 integration test 용.
func newBiomassTestApp(t *testing.T, st storage.Store) *runtime.App {
	t.Helper()
	cfg := &config.Config{}
	return runtime.NewApp(cfg, st)
}

// ── FCR table sanity ─────────────────────────────────────────────────────────

func TestFCRTableEntries(t *testing.T) {
	if len(expectedFCRTable) != 18 {
		t.Errorf("expected 18 FCR table entries, got %d", len(expectedFCRTable))
	}
}

func TestLookupFCRKnownSpecies(t *testing.T) {
	cases := []struct {
		species     string
		growthStage string
		wantFCR     float64
	}{
		{"참돔", "juvenile", 1.4},
		{"참돔", "growout", 1.6},
		{"연어", "juvenile", 1.0},
		{"연어", "growout", 1.2},
		{"광어", "growout", 1.8},
		{"우럭", "growout", 1.7},
	}
	for _, c := range cases {
		fcr, ok := LookupFCR(c.species, c.growthStage)
		if !ok {
			t.Errorf("expected ok=true for %s/%s", c.species, c.growthStage)
		}
		if math.Abs(fcr-c.wantFCR) > 0.001 {
			t.Errorf("%s/%s: got FCR=%v, want %v", c.species, c.growthStage, fcr, c.wantFCR)
		}
	}
}

func TestLookupFCRUnknown(t *testing.T) {
	fcr, ok := LookupFCR("없는어종", "growout")
	if ok {
		t.Error("expected ok=false for unknown species")
	}
	if math.Abs(fcr-DefaultFCR) > 0.001 {
		t.Errorf("expected DefaultFCR=%v, got %v", DefaultFCR, fcr)
	}
}

// ── D-4: OverrideFCR 테스트 ────────────────────────────────────────────────────

func TestProjectFromInputsUsesOverrideFCR(t *testing.T) {
	// OverrideFCR 이 설정되면 어종이 없어도 그 값 사용, FCRSource="calibrated"
	overrideFCR := 1.2
	in := ProjectionInputs{
		Species:           "미등록어종",
		GrowthStage:       "growout",
		Now:               time.Now().UTC(),
		AnchorWeightG:     50.0,
		AnchorN:           1000,
		AnchorAt:          time.Now().UTC().Add(-30 * 24 * time.Hour),
		AnchorSource:      "stocking",
		CumulativeFeedG:   10000.0,
		DaysSinceSampling: -1,
		OverrideFCR:       &overrideFCR,
		OverrideFCRSource: "calibrated",
	}
	res := ProjectFromInputs(in)

	if res.FCRSource != "calibrated" {
		t.Errorf("expected FCRSource=calibrated, got %q", res.FCRSource)
	}
	if math.Abs(res.ExpectedFCR-1.2) > 0.001 {
		t.Errorf("expected FCR=1.2, got %v", res.ExpectedFCR)
	}
	// FCRKnown=true (override 는 known 으로 취급)
	if !res.FCRKnown {
		t.Error("expected FCRKnown=true when override is set")
	}
	// 어종/단계 룩업 미스 note 없어야 함
	for _, n := range res.Notes {
		if n == "어종/단계 FCR 룩업 미스 — default 1.5 사용" {
			t.Error("override 사용 시 lookup miss note 가 없어야 함")
		}
	}
}

func TestLoadAndProjectUsesCalibratedFCR(t *testing.T) {
	st := newBiomassTestStore(t)
	ctx := context.Background()

	lc := &storage.TankLifecycle{
		TankID:            "tank_cal_01",
		ActiveStockingID:  "stocking_cal_01",
		Species:           "참돔",
		GrowthStage:       "juvenile",
		InitialCount:      1000,
		InitialAvgWeightG: 50.0,
		StockedAt:         time.Now().UTC().Add(-30 * 24 * time.Hour),
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("upsert lifecycle: %v", err)
	}

	// D-4 calibration row 삽입 (같은 stocking_id)
	cal := &storage.TankFCRCalibration{
		TankID:          "tank_cal_01",
		StockingID:      "stocking_cal_01",
		SamplingID:      "sampling_cal_01",
		DefaultFCR:      1.4,
		ObservedFCR:     1.2,
		CalibratedFCR:   1.2,
		DeviationPct:    -14.29,
		CumulativeFeedG: 10000.0,
		DeltaBiomassG:   8333.0,
		CalibratedAt:    time.Now().UTC(),
	}
	if err := st.UpsertTankFCRCalibration(ctx, cal); err != nil {
		t.Fatalf("upsert calibration: %v", err)
	}

	res, ok, err := LoadAndProject(ctx, st, "tank_cal_01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.FCRSource != "calibrated" {
		t.Errorf("expected FCRSource=calibrated, got %q", res.FCRSource)
	}
	if math.Abs(res.ExpectedFCR-1.2) > 0.001 {
		t.Errorf("expected calibrated FCR=1.2, got %v", res.ExpectedFCR)
	}
}

func TestLoadAndProjectIgnoresStaleCalibration(t *testing.T) {
	st := newBiomassTestStore(t)
	ctx := context.Background()

	lc := &storage.TankLifecycle{
		TankID:            "tank_cal_02",
		ActiveStockingID:  "stocking_new",
		Species:           "참돔",
		GrowthStage:       "juvenile",
		InitialCount:      1000,
		InitialAvgWeightG: 50.0,
		StockedAt:         time.Now().UTC().Add(-5 * 24 * time.Hour),
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	if err := st.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("upsert lifecycle: %v", err)
	}

	// 이전 cycle 의 보정값 (stocking_id 불일치)
	cal := &storage.TankFCRCalibration{
		TankID:          "tank_cal_02",
		StockingID:      "stocking_old", // 다른 cycle
		SamplingID:      "sampling_old_01",
		DefaultFCR:      1.4,
		ObservedFCR:     1.1,
		CalibratedFCR:   1.1,
		DeviationPct:    -21.43,
		CumulativeFeedG: 5000.0,
		DeltaBiomassG:   4545.0,
		CalibratedAt:    time.Now().UTC(),
	}
	if err := st.UpsertTankFCRCalibration(ctx, cal); err != nil {
		t.Fatalf("upsert calibration: %v", err)
	}

	res, ok, err := LoadAndProject(ctx, st, "tank_cal_02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	// stocking_id 불일치 → default FCR 사용
	if res.FCRSource != "default" {
		t.Errorf("expected FCRSource=default (stale calibration), got %q", res.FCRSource)
	}
	if math.Abs(res.ExpectedFCR-1.4) > 0.001 {
		t.Errorf("expected default FCR=1.4, got %v", res.ExpectedFCR)
	}
}

// ── JSON serialization sanity ────────────────────────────────────────────────

func TestProjectionResultJSON(t *testing.T) {
	res := ProjectionResult{
		EstimatedAvgWeightG: 123.4,
		AnchorSource:        "stocking",
		Quality:             "ok",
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["estimated_avg_weight_g"] == nil {
		t.Error("expected estimated_avg_weight_g in JSON")
	}
	if m["quality"] == nil {
		t.Error("expected quality in JSON")
	}
}
