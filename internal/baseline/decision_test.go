package baseline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/storage"
)

// TestDecideRouteTable — 라우팅 표 16개 셀 전체 검증.
func TestDecideRouteTable(t *testing.T) {
	cases := []struct {
		confidence float64
		mode       string
		want       Route
	}{
		// c < 0.3 — 모든 모드 rejected
		{0.1, "off", RouteRejected},
		{0.1, "observation", RouteRejected},
		{0.1, "partial", RouteRejected},
		{0.1, "full", RouteRejected},
		// 0.3 ≤ c < 0.6
		{0.45, "off", RoutePendingApproval},
		{0.45, "observation", RouteAdvisoryOnly},
		{0.45, "partial", RoutePendingApproval},
		{0.45, "full", RoutePendingApproval},
		// 0.6 ≤ c < 0.85
		{0.72, "off", RoutePendingApproval},
		{0.72, "observation", RouteAdvisoryOnly},
		{0.72, "partial", RoutePendingNotify},
		{0.72, "full", RoutePendingNotify},
		// c ≥ 0.85
		{0.90, "off", RoutePendingApproval},
		{0.90, "observation", RouteAdvisoryOnly},
		{0.90, "partial", RoutePendingNotify},
		{0.90, "full", RouteAutoExecuted},
	}
	for _, c := range cases {
		got, reasoning := DecideRoute(RouteDecisionInputs{
			TankID:         "tank_test",
			Confidence:     c.confidence,
			AutonomousMode: c.mode,
		})
		if got != c.want {
			t.Errorf("confidence=%.2f mode=%s: got %q, want %q (reasoning: %s)",
				c.confidence, c.mode, got, c.want, reasoning)
		}
		if reasoning == "" {
			t.Errorf("confidence=%.2f mode=%s: reasoning empty", c.confidence, c.mode)
		}
	}
}

// TestDecideRouteEdgeCases — 경계값 정확 검증 (0.3, 0.6, 0.85).
func TestDecideRouteEdgeCases(t *testing.T) {
	cases := []struct {
		confidence float64
		mode       string
		want       Route
	}{
		// 경계: 0.3 정확히 → cold 아님, observation 밴드 시작
		{0.3, "off", RoutePendingApproval},
		{0.3, "observation", RouteAdvisoryOnly},
		{0.3, "partial", RoutePendingApproval},
		{0.3, "full", RoutePendingApproval},
		// 경계: 0.6 정확히 → adapted 밴드 시작
		{0.6, "partial", RoutePendingNotify},
		{0.6, "full", RoutePendingNotify},
		// 경계: 0.85 정확히 → autonomous 밴드 시작
		{0.85, "partial", RoutePendingNotify},
		{0.85, "full", RouteAutoExecuted},
		// cold 상한 바로 아래
		{0.299, "full", RouteRejected},
		// adapted 상한 바로 아래
		{0.849, "full", RoutePendingNotify},
	}
	for _, c := range cases {
		got, _ := DecideRoute(RouteDecisionInputs{
			TankID:         "tank_test",
			Confidence:     c.confidence,
			AutonomousMode: c.mode,
		})
		if got != c.want {
			t.Errorf("confidence=%.3f mode=%s: got %q, want %q",
				c.confidence, c.mode, got, c.want)
		}
	}
}

// findMigDir — decision_test 에서 migrations 디렉터리 탐색.
func findDecisionMigDir(t *testing.T) string {
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

func openTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	migDir := findDecisionMigDir(t)
	for _, name := range []string{"001_init.sql", "002_autonomous_mode.sql"} {
		if err := storage.Migrate(st, filepath.Join(migDir, name)); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

// TestLoadRoutingInputsMissingMode — 모드 미설정 시 "off" 기본값 적용.
func TestLoadRoutingInputsMissingMode(t *testing.T) {
	st := openTestStore(t)
	in, err := LoadRoutingInputs(context.Background(), st, "tank_nomode")
	if err != nil {
		t.Fatalf("LoadRoutingInputs error: %v", err)
	}
	if in.AutonomousMode != "off" {
		t.Errorf("expected mode=off (default), got %q", in.AutonomousMode)
	}
	if in.TankID != "tank_nomode" {
		t.Errorf("expected tank_id=tank_nomode, got %q", in.TankID)
	}
}

// TestLoadRoutingInputsLowConfidence — 빈 store → confidence=0 → route=rejected.
func TestLoadRoutingInputsLowConfidence(t *testing.T) {
	st := openTestStore(t)
	in, err := LoadRoutingInputs(context.Background(), st, "tank_empty")
	if err != nil {
		t.Fatalf("LoadRoutingInputs error: %v", err)
	}
	// 빈 store → composite=0
	if in.Confidence != 0 {
		t.Errorf("expected confidence=0 for empty store, got %f", in.Confidence)
	}
	route, _ := DecideRoute(in)
	if route != RouteRejected {
		t.Errorf("expected rejected for confidence=0, got %q", route)
	}
}
