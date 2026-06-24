package vision_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/vision"
)

// withChdir temporarily switches working directory so ManifestPath (which is
// relative) lands under a tmpdir. Restored on cleanup.
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

// makeWeights creates a non-empty file that can pass os.Stat checks.
func makeWeights(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("dummy weights"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestPromoteAndRollback(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)

	w1 := makeWeights(t, filepath.Join(tmp, "runs/job1/best.pt"))
	w2 := makeWeights(t, filepath.Join(tmp, "runs/job2/best.pt"))

	// 1) 첫 promote → 활성 weights 설정, history 비어있음
	st, err := vision.Promote("algo_a", w1, "job1", "op1")
	if err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if st.ActiveWeightsPath != w1 || len(st.History) != 0 {
		t.Fatalf("first promote state wrong: %+v", st)
	}

	// 2) 두 번째 promote → 이전 것은 history 로
	st, err = vision.Promote("algo_a", w2, "job2", "op2")
	if err != nil {
		t.Fatalf("second promote: %v", err)
	}
	if st.ActiveWeightsPath != w2 {
		t.Fatalf("second promote active path: %q", st.ActiveWeightsPath)
	}
	if len(st.History) != 1 || st.History[0].WeightsPath != w1 {
		t.Fatalf("history not preserved: %+v", st.History)
	}

	// 3) rollback → 이전 weights 로 복원, history 1 건 소비
	st, err = vision.Rollback("algo_a", "op3")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if st.ActiveWeightsPath != w1 {
		t.Fatalf("rollback restored wrong path: %q", st.ActiveWeightsPath)
	}
	if len(st.History) != 0 {
		t.Fatalf("history should be empty after single-step rollback: %+v", st.History)
	}

	// 4) history 빈 상태에서 rollback → ErrNoHistory
	if _, err := vision.Rollback("algo_a", "op4"); !errors.Is(err, vision.ErrNoHistory) {
		t.Fatalf("expected ErrNoHistory, got %v", err)
	}
}

func TestPromoteRejectsMissingWeights(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)

	if _, err := vision.Promote("algo_a", filepath.Join(tmp, "nope.pt"), "j", "op"); err == nil {
		t.Fatalf("expected error for missing weights file")
	}
}

func TestActiveStateEmpty(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	st, err := vision.ActiveState("algo_unknown")
	if err != nil {
		t.Fatalf("ActiveState empty: %v", err)
	}
	if st.ActiveWeightsPath != "" {
		t.Fatalf("expected zero state, got %+v", st)
	}
}

// makeBaselineDir creates a fake baseline model directory that passes
// PromoteTankBaseline 's existence check.
func makeBaselineDir(t *testing.T, base string) string {
	t.Helper()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "model.pt"), []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return base
}

func TestPromoteAndRollbackTankBaseline(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)

	dir1 := makeBaselineDir(t, filepath.Join(tmp, "baselines/tank_a/job1"))
	dir2 := makeBaselineDir(t, filepath.Join(tmp, "baselines/tank_a/job2"))

	st, err := vision.PromoteTankBaseline("tank_a", dir1, "job1", "auto")
	if err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if st.ActiveWeightsPath != dir1 || len(st.History) != 0 {
		t.Fatalf("first promote state: %+v", st)
	}

	st, err = vision.PromoteTankBaseline("tank_a", dir2, "job2", "auto")
	if err != nil {
		t.Fatalf("second promote: %v", err)
	}
	if st.ActiveWeightsPath != dir2 || len(st.History) != 1 {
		t.Fatalf("second promote: %+v", st)
	}
	if st.History[0].WeightsPath != dir1 {
		t.Fatalf("history not preserved: %+v", st.History)
	}

	// Vision 의 algorithm-level Promote 와 격리 — 같은 ID 라도 영향 X
	if vstate, _ := vision.ActiveState("tank_a"); vstate.ActiveWeightsPath != "" {
		t.Fatalf("tank baseline 이 algorithm Active 에 침범됨: %+v", vstate)
	}

	// Rollback
	st, err = vision.RollbackTankBaseline("tank_a", "op")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if st.ActiveWeightsPath != dir1 {
		t.Fatalf("rollback restored wrong path: %q", st.ActiveWeightsPath)
	}

	// History empty → ErrNoHistory
	if _, err := vision.RollbackTankBaseline("tank_a", "op"); !errors.Is(err, vision.ErrNoHistory) {
		t.Fatalf("expected ErrNoHistory, got %v", err)
	}
}

func TestActiveTankBaselineEmpty(t *testing.T) {
	tmp := t.TempDir()
	withChdir(t, tmp)
	st, err := vision.ActiveTankBaseline("tank_unknown")
	if err != nil {
		t.Fatalf("ActiveTankBaseline empty: %v", err)
	}
	if st.ActiveWeightsPath != "" {
		t.Fatalf("expected zero state, got %+v", st)
	}
}
