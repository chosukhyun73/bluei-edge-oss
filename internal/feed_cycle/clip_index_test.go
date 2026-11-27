package feed_cycle

import (
	"strings"
	"testing"
)

func clipIndexTestReporter() *Reporter {
	rp := &Reporter{}
	// r3 스케일 기준선 (204사이클 실측, configs/edge.example.yaml 과 동일).
	rp.SetClipIndex(
		map[string]float64{"tank_01": 0.635, "tank_02": 0.368},
		map[string]float64{"tank_01": 0.740, "tank_02": 0.524},
		0.928, 1.015)
	return rp
}

func TestClipState(t *testing.T) {
	rp := clipIndexTestReporter()
	cases := []struct {
		name      string
		tank      string
		level     float64
		direction float64
		want      string
	}{
		// r3 스케일. 강도 P25 tank_01 0.635 / tank_02 0.368, 방향 0.928~1.015.
		{"끝까지 지속", "tank_01", 0.780, 1.050, clipStateSustained},
		{"후반 급락", "tank_02", 0.480, 0.870, clipStateDrop},
		{"통상 감소", "tank_01", 0.700, 0.975, clipStateNormal},
		{"강도 미달", "tank_02", 0.300, 0.975, clipStateLow},
		// 강도가 낮으면 방향이 좋아도 "낮음"이 이긴다 — 적게 먹은 사이클에서
		// 하강 여부는 의미가 없다.
		{"강도 미달이 방향을 이긴다", "tank_02", 0.200, 1.200, clipStateLow},
		// 경계값은 포함/미포함이 뒤바뀌면 안 된다.
		{"방향 상한 경계", "tank_01", 0.700, 1.015, clipStateSustained},
		{"방향 하한 경계", "tank_01", 0.700, 0.928, clipStateNormal},
		// 기준선이 없는 수조는 강도 게이트를 건너뛴다(미캘리브레이션).
		{"미등록 수조", "tank_09", 0.010, 1.050, clipStateSustained},
	}
	for _, c := range cases {
		got, msg := rp.clipState(c.tank, c.level, c.direction)
		if got != c.want {
			t.Errorf("%s: state = %s, want %s", c.name, got, c.want)
		}
		if msg == "" {
			t.Errorf("%s: message 가 비었다", c.name)
		}
	}
}

func TestClipRecommendation(t *testing.T) {
	rp := clipIndexTestReporter()
	cases := []struct {
		name           string
		tank           string
		level          float64
		direction      float64
		wantAmount     float64
		wantInterval   float64
		wantSummaryHas string
	}{
		// 두 축 모두 중간 구간 — 아무것도 안 건드린다.
		{"통상 사이클", "tank_01", 0.700, 0.975, 1.0, 1.0, "양 유지 · 간격 유지"},
		// 강도가 P75(0.740) 초과인데 방향이 급락 — 한 번에 적게, 대신 자주.
		{"강하게 시작해 급락", "tank_01", 0.780, 0.900, clipRecoAmountDown, clipRecoIntervalDown, "양 -10%"},
		// 강도가 P25~P75 사이면 간격은 안 건드리고 방향만 반영한다.
		{"방향만 급락", "tank_02", 0.450, 0.900, clipRecoAmountDown, 1.0, "간격 유지"},
		// 강도 미달 — 간격을 늦춘다.
		{"강도 미달", "tank_01", 0.600, 0.900, clipRecoAmountDown, clipRecoIntervalUp, "간격 +10%"},
		// 끝까지 반응이 남고 평소보다 배고팠던 사이클 — 양 늘리고 간격 당긴다.
		{"부족했던 사이클", "tank_01", 0.800, 1.050, clipRecoAmountUp, clipRecoIntervalDown, "양 +10%"},
		// P75 가 없는 수조는 "당김"이 나오면 안 된다 — 미캘리브레이션 보호.
		{"미등록 수조", "tank_09", 9.99, 0.975, 1.0, 1.0, "간격 유지"},
	}
	for _, c := range cases {
		got := rp.clipRecommendation(c.tank, c.level, c.direction)
		if got["amount_factor"] != c.wantAmount {
			t.Errorf("%s: amount = %v, want %v", c.name, got["amount_factor"], c.wantAmount)
		}
		if got["interval_factor"] != c.wantInterval {
			t.Errorf("%s: interval = %v, want %v", c.name, got["interval_factor"], c.wantInterval)
		}
		if s, _ := got["summary"].(string); !strings.Contains(s, c.wantSummaryHas) {
			t.Errorf("%s: summary = %q, want contains %q", c.name, s, c.wantSummaryHas)
		}
		// 표기 전용 — 급이에 반영되지 않는다는 표식이 반드시 있어야 한다.
		if got["applied"] != false {
			t.Errorf("%s: applied = %v, want false", c.name, got["applied"])
		}
	}
}

func TestClipStateUnconfigured(t *testing.T) {
	// 임계 미설정이면 지수를 내지 않는다 — clipIndexBlock 의 가드.
	rp := &Reporter{}
	if rp.clipIndexDirLow > 0 || rp.clipIndexDirHigh > 0 {
		t.Fatal("기본값은 0 이어야 한다")
	}
}

// 클램프가 방향의 부호를 지켜야 한다 — 음수 분자가 나오면 비율이 의미를 잃는다.
func TestDirectionNeverNegative(t *testing.T) {
	// 08-07 12:00 tank_02 실측 상승폭 (p2 는 no 로 제외된 뒤 남은 6발).
	rises := []float64{2.084, 2.644, 1.344, 1.464, 0, 0} // 뒤 두 발이 클램프된 값
	k := len(rises) / 3
	if k < 2 {
		k = 2
	}
	head := meanOf(rises[:k])
	direction := meanOf(rises[len(rises)-k:]) / head
	if direction < 0 {
		t.Fatalf("direction = %v, 음수가 나오면 안 된다", direction)
	}
	if direction != 0 {
		t.Fatalf("direction = %v, want 0 (뒤 두 발 모두 클램프)", direction)
	}
}

func TestDischargeVerdictsOf(t *testing.T) {
	if got := dischargeVerdictsOf(nil); got != nil {
		t.Errorf("nil 블록 = %v, want nil", got)
	}
	if got := dischargeVerdictsOf(map[string]any{}); got != nil {
		t.Errorf("빈 블록 = %v, want nil", got)
	}
	got := dischargeVerdictsOf(map[string]any{"per_pulse": []string{"yes", "no"}})
	if len(got) != 2 || got[1] != "no" {
		t.Errorf("per_pulse = %v", got)
	}
}

func TestMedian(t *testing.T) {
	if got := median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("홀수 = %v, want 2", got)
	}
	if got := median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("짝수 = %v, want 2.5", got)
	}
	if got := median(nil); got != 0 {
		t.Errorf("빈 = %v, want 0", got)
	}
}
