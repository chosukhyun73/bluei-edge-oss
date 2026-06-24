package baseline_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision"
)

// withChdir + findMigPath — 다른 테스트와 동일 패턴 (api/storage 와 중복 OK).
func withChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func findMigPath(t *testing.T) string {
	t.Helper()
	p, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		c := filepath.Join(p, "migrations", "001_init.sql")
		if _, err := os.Stat(c); err == nil {
			return c
		}
		p = filepath.Dir(p)
	}
	t.Fatal("migrations/001_init.sql not found")
	return ""
}

func newTestEnv(t *testing.T) (*runtime.App, storage.Store) {
	t.Helper()
	// chdir 전에 migration 절대 경로 확보
	migPath := findMigPath(t)

	tmp := t.TempDir()
	withChdir(t, tmp)

	dbPath := filepath.Join(tmp, "test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// 001~008 순차 적용 (IF NOT EXISTS — 이중 적용 안전).
	migDir := filepath.Dir(migPath)
	for _, name := range []string{
		"001_init.sql", "002_autonomous_mode.sql", "003_lifecycle.sql",
		"004_sampling.sql", "005_fcr_calibration.sql", "006_decision_policy.sql",
		"007_weight_history.sql", "008_groups.sql",
		"009_farms_sites.sql", "010_water_treatment_groups.sql", "011_controllers.sql",
		"012_actuators_sensors.sql", "013_species_profiles.sql",
		"014_feeding_schedules.sql", "015_predictive_events.sql",
		"016_learned_safety.sql", "017_arbiter_decisions.sql",
		"018_arbiter_preemption.sql", "019_feed_cycle_intent.sql",
		"020_sync_batch_event_seq_index.sql", "021_tank_physical_dimensions.sql",
		"022_camera_models.sql", "023_camera_view_geometry.sql",
		"024_sensor_models.sql",
		"025_actuator_models.sql",
		"030_traceability_lifecycle.sql",
		"031_species_fao.sql",
	} {
		p := filepath.Join(migDir, name)
		if _, err := os.Stat(p); err != nil {
			continue // 파일 없으면 스킵
		}
		if err := storage.Migrate(st, p); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}

	cfg := &config.Config{}
	cfg.Site.SiteID = "site_test"
	cfg.Edge.EdgeID = "edge_test"
	app := runtime.NewApp(cfg, st)
	return app, st
}

// TestWorkerDisabledStartsCleanly — Enabled=false 면 즉시 done 되고 Stop 빠르게.
func TestWorkerDisabledStartsCleanly(t *testing.T) {
	app, st := newTestEnv(t)
	w := baseline.NewWorker(app, st, baseline.NewScorer("test.db"),
		baseline.Config{Enabled: false})
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestWorkerSkipsTanksWithoutModel — 모델 없는 Cage/Tank는 score 시도 안 하고 skip.
// (subprocess 호출 자체가 일어나지 않아야 — InitialDelay 짧게 + 즉시 Stop)
func TestWorkerSkipsTanksWithoutModel(t *testing.T) {
	app, st := newTestEnv(t)
	// Cage/Tank 등록만 — baseline 모델 없음
	if err := st.UpsertTankProfile(context.Background(), &storage.TankProfile{
		TankID: "tank_a", DisplayName: "A", Species: "참돔",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Scorer 의 ScriptPath 를 존재하지 않는 경로로 → 만약 호출되면 즉시 실패하지만
	// 우리는 호출 자체를 안 하는지 확인하므로 무관. tick 이 ActiveTankBaseline 에서
	// 빈 결과 받고 skip 해야 함.
	w := baseline.NewWorker(app, st, baseline.NewScorer("test.db"),
		baseline.Config{
			Enabled:      true,
			Interval:     100 * time.Millisecond,
			InitialDelay: 10 * time.Millisecond,
		})
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 첫 tick 대기 (InitialDelay 10ms + 잠깐)
	time.Sleep(60 * time.Millisecond)
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// tank.baseline.scored 이벤트가 0건이어야 함 (모델 없어서 skip 되었으니까)
	es, err := st.QueryEvents(context.Background(), storage.EventFilter{
		EventType: "tank.baseline.scored", Limit: 10,
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(es) != 0 {
		t.Errorf("expected 0 score events, got %d", len(es))
	}
}

// TestWorkerStopsImmediatelyWithCancel — Stop(ctx) 가 ctx 만료에 즉시 응답.
func TestWorkerStopsImmediatelyWithCancel(t *testing.T) {
	app, st := newTestEnv(t)
	w := baseline.NewWorker(app, st, baseline.NewScorer("test.db"),
		baseline.Config{
			Enabled:      true,
			Interval:     1 * time.Hour, // 길게
			InitialDelay: 1 * time.Hour, // 즉시 시작 안 함
		})
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t0 := time.Now()
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(t0); elapsed > 500*time.Millisecond {
		t.Errorf("Stop took %v, expected < 500ms", elapsed)
	}
}

// TestWorkerReadsTankBaselineManifest — manifest 에 baseline 등록된 Cage/Tank는
// score 시도. subprocess 가 실패하더라도 worker 자체는 죽지 않음.
func TestWorkerReadsTankBaselineManifest(t *testing.T) {
	app, st := newTestEnv(t)
	if err := st.UpsertTankProfile(context.Background(), &storage.TankProfile{
		TankID: "tank_b", DisplayName: "B", Species: "참돔",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// 가짜 model 디렉터리 + 파일
	cwd, _ := os.Getwd()
	modelDir := filepath.Join(cwd, "fake_model")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.pt"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := vision.PromoteTankBaseline("tank_b", modelDir, "job_x", "auto"); err != nil {
		t.Fatalf("PromoteTankBaseline: %v", err)
	}
	// scorer 의 ScriptPath 를 missing 으로 — subprocess 즉시 실패
	scorer := baseline.NewScorer(filepath.Join(cwd, "test.db"))
	scorer.ScriptPath = "nonexistent_script.py"

	w := baseline.NewWorker(app, st, scorer, baseline.Config{
		Enabled:      true,
		Interval:     1 * time.Hour,
		InitialDelay: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 첫 tick 까지 대기
	time.Sleep(200 * time.Millisecond)
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// subprocess 실패해도 worker 가 panic/crash 안 했으면 OK.
	// 결과 이벤트는 0건 (실패는 log only).
	es, _ := st.QueryEvents(context.Background(), storage.EventFilter{
		EventType: "tank.baseline.scored", Limit: 10,
	})
	if len(es) != 0 {
		t.Errorf("expected 0 events on subprocess failure, got %d", len(es))
	}
}

// TestWorkerTransitionAutoDowngradesMode — 전환 감지 시 mode=full → observation 자동 다운그레이드.
// 체중 임계(120g > 100g) 조건으로 DetectGrowthTransition 발화 → checkTransition → autoDowngradeOnTransition.
func TestWorkerTransitionAutoDowngradesMode(t *testing.T) {
	app, st := newTestEnv(t)
	cwd, _ := os.Getwd()

	// Cage/Tank 등록 (avg_weight_g=120 → 100g 임계 첫 통과)
	if err := st.UpsertTankProfile(context.Background(), &config.TankProfile{
		TankID:      "tank_wd",
		DisplayName: "WD",
		Species:     "참돔",
		AvgWeightG:  120.0,
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	// 자율 모드 full 설정
	if err := st.UpsertTankAutonomousMode(context.Background(), &storage.TankAutonomousMode{
		TankID:    "tank_wd",
		Mode:      "full",
		Reason:    "test",
		ChangedAt: time.Now().UTC(),
		ChangedBy: "test",
	}); err != nil {
		t.Fatalf("upsert mode: %v", err)
	}

	// baseline 모델 manifest 등록 — checkTransition 대상 Cage/Tank가 되려면 필요
	modelDir := filepath.Join(cwd, "fake_model_wd")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.pt"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := vision.PromoteTankBaseline("tank_wd", modelDir, "job_wd", "auto"); err != nil {
		t.Fatalf("PromoteTankBaseline: %v", err)
	}

	// 이미 이전 weight_threshold 전환 이벤트가 없으므로 DetectGrowthTransition 은 weight_threshold_passed 반환.
	// recentTransitionExists 도 통과 → checkTransition 에서 emit + raiseAlert + autoDowngrade.

	// scorer ScriptPath 없음 → scoreOne 은 실패하지만 checkTransition 은 독립 실행됨
	scorer := baseline.NewScorer(filepath.Join(cwd, "test.db"))
	scorer.ScriptPath = "nonexistent_script.py"

	w := baseline.NewWorker(app, st, scorer, baseline.Config{
		Enabled:      true,
		Interval:     1 * time.Hour,
		InitialDelay: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // tick 완료 대기
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// 모드가 observation 으로 바뀌었어야 함
	row, err := st.GetTankAutonomousMode(context.Background(), "tank_wd")
	if err != nil {
		t.Fatalf("GetTankAutonomousMode: %v", err)
	}
	if row == nil || row.Mode != "observation" {
		got := "nil"
		if row != nil {
			got = row.Mode
		}
		t.Errorf("expected mode=observation after transition, got %q", got)
	}

	// auto_downgrade 이벤트가 적재됐는지 확인
	evts, qErr := st.QueryEvents(context.Background(), storage.EventFilter{
		EventType: events.EventAutonomousModeAutoDowngrade,
		Limit:     10,
	})
	if qErr != nil {
		t.Fatalf("query downgrade events: %v", qErr)
	}
	found := false
	for _, e := range evts {
		var p events.AutonomousModeAutoDowngradePayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == "tank_wd" && p.PreviousMode == "full" && p.NewMode == "observation" && p.Reason == "transition_detected" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected auto_downgrade event with full→observation for tank_wd")
	}
}
