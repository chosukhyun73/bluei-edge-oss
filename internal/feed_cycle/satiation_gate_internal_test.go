package feed_cycle

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

// 보상 펄스 만복 게이트 — 복귀(만복)면 skip, 미복귀(배고픔)면 보상 진행.
func TestCompensationSatiated(t *testing.T) {
	mkSeries := func(base, tail float64) []float64 {
		s := make([]float64, 120)
		for i := range s {
			s[i] = base
			if i%2 == 1 {
				s[i] += 0.2 // 노이즈
			}
		}
		for i := 110; i < 120; i++ {
			s[i] = tail // 끝 구간 활동도
		}
		return s
	}
	newCycle := func() *Cycle {
		c := NewFixedCycle("cycle_sat", "tank_02", "", FixedParams{
			PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
		})
		for i := 0; i < 5; i++ {
			c.recordPulseResult(18.8, 18.8)
		}
		c.grantCompensation(18.8) // 보상 1발 승인 → 다음 펄스 = 보상
		return c
	}

	w := &Worker{}

	// liveSeriesFn 미설정 → 게이트 비활성 (보상 진행)
	if sat := w.compensationSatiated(newCycle()); sat.satiated {
		t.Fatal("no provider: expected no skip")
	}

	// 만복 (끝 활동 ≈ 기저) → skip
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) {
		return mkSeries(5.0, 5.1), time.Time{}, true
	}
	if sat := w.compensationSatiated(newCycle()); !sat.satiated {
		t.Fatal("satiated: expected skip")
	}

	// 미복귀 (끝 활동 = 기저 ×1.8) → 보상 진행
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) {
		return mkSeries(5.0, 9.0), time.Time{}, true
	}
	if sat := w.compensationSatiated(newCycle()); sat.satiated {
		t.Fatal("still hungry: expected no skip")
	}

	// 원 계획 내 펄스(보상 아님) → 게이트 대상 아님
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) {
		return mkSeries(5.0, 5.1), time.Time{}, true
	}
	c := NewFixedCycle("cycle_plan", "tank_02", "", FixedParams{
		PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
	})
	c.recordPulseResult(18.8, 18.8)
	c.grantCompensation(18.8)
	if sat := w.compensationSatiated(c); sat.satiated {
		t.Fatal("within planned pulses: expected no skip")
	}
}

// 게이트 평가창 지연 — 직전 펄스 요동 미정착이면 폴링 대기 후 평가 (7-09 17:00
// 경계 오발사 근본 대책).
func TestWaitCompensationSettle(t *testing.T) {
	stable := make([]float64, 120)
	for i := range stable {
		stable[i] = 5.0
		if i%2 == 1 {
			stable[i] += 0.2
		}
	}
	decaying := append([]float64(nil), stable...)
	for i, v := range []float64{20, 18, 16, 14, 12, 10, 9, 8, 7, 6} {
		decaying[110+i] = v // 감쇠 꼬리: prev5≈16 vs last5≈8
	}
	c := NewFixedCycle("cycle_settle", "tank_02", "", FixedParams{
		PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
	})

	// 정착 상태 → 대기 없이 즉시 진행
	w := &Worker{liveSeriesFn: func(string) ([]float64, time.Time, bool) {
		return stable, time.Time{}, true
	}}
	if res, waited, settled := w.waitCompensationSettle(c); res != gapElapsed || waited != 0 || !settled {
		t.Fatalf("stable: expected immediate settled, got res=%v waited=%d settled=%v", res, waited, settled)
	}

	// 감쇠 꼬리 → 1회 폴링 대기 후 정착 확인 (시리즈가 시간 경과로 진정되는 상황 모사)
	calls := 0
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) {
		calls++
		if calls == 1 {
			return decaying, time.Time{}, true
		}
		return stable, time.Time{}, true
	}
	if res, waited, settled := w.waitCompensationSettle(c); res != gapElapsed || waited != compensationSettlePollMs || !settled {
		t.Fatalf("decaying: expected settle after one poll, got res=%v waited=%d settled=%v", res, waited, settled)
	}

	// 시리즈 없음 → 판정 불가, 대기 없이 진행
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) {
		return nil, time.Time{}, false
	}
	if res, waited, settled := w.waitCompensationSettle(c); res != gapElapsed || waited != 0 || settled {
		t.Fatalf("no series: expected immediate pass-through, got res=%v waited=%d settled=%v", res, waited, settled)
	}
}

// 회귀 fixture — 2026-07-09 17:00 tank_02 만복 게이트 경계 오발사(p6) 실사이클.
// 직전 펄스 요동 감쇠 꼬리 위에서 평가하면 last5 가 부풀어 게이트가 보상을 통과
// (오발사)시키고, 정착 후 평가하면 만복으로 정상 차단됨을 실데이터로 고정한다.
func TestSettleGateRegression20260709(t *testing.T) {
	raw, err := os.ReadFile("testdata/agitation_live_tank02_20260709_1700.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Series []float64 `json:"series"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	newCycle := func() *Cycle {
		c := NewFixedCycle("cycle_0709", "tank_02", "", FixedParams{
			PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
		})
		for i := 0; i < 5; i++ {
			c.recordPulseResult(18.8, 18.8)
		}
		c.grantCompensation(18.8)
		return c
	}

	// 감쇠 꼬리 구간 (+341s): 미정착 + 게이트는 배고픔으로 오판(통과 = 오발사 재현)
	w := &Worker{liveSeriesFn: func(string) ([]float64, time.Time, bool) {
		return fx.Series[:341], time.Time{}, true
	}}
	if settled, ok := w.compensationAgitationSettled(newCycle()); !ok || settled {
		t.Fatalf("t=341: expected unsettled (decay tail), got settled=%v ok=%v", settled, ok)
	}
	if sat := w.compensationSatiated(newCycle()); sat.satiated {
		t.Fatalf("t=341: gate would have fired in reality (last5=%.2f base=%.2f)", sat.last5Mean, sat.baseline)
	}

	// 정착 후 (+348s): 정착 확인 + 게이트가 만복으로 정상 차단
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) {
		return fx.Series[:348], time.Time{}, true
	}
	if settled, ok := w.compensationAgitationSettled(newCycle()); !ok || !settled {
		t.Fatalf("t=348: expected settled, got settled=%v ok=%v", settled, ok)
	}
	if sat := w.compensationSatiated(newCycle()); !sat.satiated {
		t.Fatalf("t=348: expected satiation skip (last5=%.2f base=%.2f ratio=%.3f)", sat.last5Mean, sat.baseline, sat.last5Mean/sat.baseline)
	}
}

// 사이클 피크 만복 게이트 (2026-07-20) — 저활동 사이클(피크 낮음)만 만복 폐기,
// 활발 사이클(피크 높음)은 보상 통과. gap끝 복귀도와 달리 저진폭 카메라에서도
// 배고픔/저활동을 가른다. 7-20 tank_02 실측 근거(저녁 저활동 0.22 vs 낮 활발 0.36+).
func TestCompensationSatiated_PeakMode(t *testing.T) {
	// 카메라 P90 산정용 요동 이벤트 20건 주입 (탑뷰, rise ~0.5) → P90≈0.5.
	store := &peakGateStore{}
	for i := 0; i < 20; i++ {
		store.agitation = append(store.agitation,
			`{"tank_id":"tank_02","camera_id":"cam_top_7_1_2_R","baseline":10,"peak":15}`) // rise 0.5
	}
	cfg := TestConfig()
	cfg.CompensationSatiationPeakThreshold = 0.30
	w := &Worker{cfg: cfg, store: store}

	newCycle := func() *Cycle {
		c := NewFixedCycle("cycle_peak", "tank_02", "", FixedParams{
			PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
		})
		for i := 0; i < 5; i++ {
			c.recordPulseResult(18.8, 18.8)
		}
		c.grantCompensation(18.8)
		return c
	}
	// 요동 series: 기저 10 + 피크로 상승. peak/baseline-1 = rise, norm=rise/P90(0.5).
	mkSeries := func(peak float64) []float64 {
		s := make([]float64, 120)
		for i := range s {
			s[i] = 10
		}
		for i := 40; i < 60; i++ {
			s[i] = peak // 사이클 중반 피크
		}
		return s
	}

	// 저활동: peak 11 → rise 0.1 → norm 0.2 < 0.30 → 만복 폐기 (저녁 저활동 재현)
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) { return mkSeries(11), time.Time{}, true }
	if sat := w.compensationSatiated(newCycle()); !sat.satiated || sat.mode != "peak" {
		t.Fatalf("low activity: expected peak-mode skip, got %+v", sat)
	}
	// 활발: peak 15 → rise 0.5 → norm 1.0 ≥ 0.30 → 보상 통과 (배고픔)
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) { return mkSeries(15), time.Time{}, true }
	if sat := w.compensationSatiated(newCycle()); sat.satiated {
		t.Fatalf("high activity: expected no skip, got %+v", sat)
	}

	// P90 미확보(요동 이벤트 부족) → legacy gap끝 게이트 폴백
	w.store = &peakGateStore{}
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) { return mkSeries(11), time.Time{}, true }
	if sat := w.compensationSatiated(newCycle()); sat.mode != "gap_end" {
		t.Fatalf("no P90: expected gap_end fallback, got mode=%q", sat.mode)
	}
}

// 만복 전제조건 — 그 사이클에 배출이 K발 확인되기 전에는 만복 게이트를 걸지 않는다.
// 맥락 게이트(worker.go:1004)와 같은 조건·같은 설정값. 8-03 10:00 tank_02 재현:
// 6발 중 배출 1발인 급이기 고장 사이클에서 저활동(peak_norm 0.14)을 만복으로 읽고
// 승인된 보상 2발을 취소해 37.6g 로 끝났다.
func TestCompensationSatiated_MinConfirmedPrecondition(t *testing.T) {
	store := &peakGateStore{}
	for i := 0; i < 20; i++ {
		store.agitation = append(store.agitation,
			`{"tank_id":"tank_02","camera_id":"cam_top_7_1_2_R","baseline":10,"peak":15}`) // P90 0.5
	}
	cfg := TestConfig()
	cfg.CompensationSatiationPeakThreshold = 0.30
	cfg.CompensationGateMinConfirmed = 3
	w := &Worker{cfg: cfg, store: store}

	// 저활동 series — 이 자체로는 만복(peak 11 → rise 0.1 → norm 0.2 < 0.30) 판정 대상.
	lowActivity := make([]float64, 120)
	for i := range lowActivity {
		lowActivity[i] = 10
	}
	for i := 40; i < 60; i++ {
		lowActivity[i] = 11
	}
	w.liveSeriesFn = func(string) ([]float64, time.Time, bool) { return lowActivity, time.Time{}, true }

	newCycle := func(confirmed int) *Cycle {
		c := NewFixedCycle("cycle_k", "tank_02", "", FixedParams{
			PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
		})
		for i := 0; i < 5; i++ {
			c.recordPulseResult(18.8, 18.8)
		}
		for i := 0; i < confirmed; i++ {
			c.recordConfirmedDischarge()
		}
		c.grantCompensation(18.8)
		return c
	}

	// 배출 1발뿐 = 급이기 고장 의심 → 만복일 수 없으므로 게이트 미적용(보상 진행).
	for _, confirmed := range []int{0, 1, 2} {
		if sat := w.compensationSatiated(newCycle(confirmed)); sat.satiated {
			t.Errorf("confirmed=%d: 만복 판정이 나오면 안 된다 (급이기 고장을 만복으로 오독), got %+v",
				confirmed, sat)
		}
	}
	// 배출 3발 확인 = 배부를 수 있는 상태 → 종전대로 만복 게이트 작동.
	if sat := w.compensationSatiated(newCycle(3)); !sat.satiated || sat.mode != "peak" {
		t.Errorf("confirmed=3: 만복 게이트가 작동해야 한다, got %+v", sat)
	}
}

// peakGateStore — tankTopviewRiseP90 의 QueryEvents 만 쓰는 최소 mock.
type peakGateStore struct {
	storage.Store
	agitation []string
}

func (s *peakGateStore) GetTankAutonomousMode(_ context.Context, tankID string) (*storage.TankAutonomousMode, error) {
	return &storage.TankAutonomousMode{TankID: tankID, Mode: "observation"}, nil
}

func (s *peakGateStore) QueryEvents(_ context.Context, f storage.EventFilter) ([]*storage.Event, error) {
	if f.EventType != vision_pipeline.EventAgitationRecorded {
		return nil, nil
	}
	out := make([]*storage.Event, len(s.agitation))
	for i, pj := range s.agitation {
		out[i] = &storage.Event{PayloadJSON: pj}
	}
	return out, nil
}

// P90 하한 — 활동 저하로 계수가 무너져도 과증폭되지 않아야 한다 (2026-07-31).
// 실측 배경: tank_02 P90 이 0.885 → 0.454 로 45% 하락해 증폭이 1.13 → 2.20배로 뛰었고,
// 만복 게이트가 저활동 사이클도 "활발"로 읽게 됐다.
func TestTankTopviewRiseP90_Floor(t *testing.T) {
	// rise 0.3 짜리 20건 → P90 = 0.3 (하한 0.70 아래)
	store := &peakGateStore{}
	for i := 0; i < 20; i++ {
		store.agitation = append(store.agitation,
			`{"tank_id":"tank_02","camera_id":"cam_top_7_1_2_R","baseline":10,"peak":13}`)
	}
	cfg := TestConfig()
	cfg.SatiationRiseP90Floor = 0.70
	w := &Worker{cfg: cfg, store: store}

	got, ok := w.tankTopviewRiseP90("tank_02")
	if !ok {
		t.Fatal("P90 산정 가능해야 한다")
	}
	if got != 0.70 {
		t.Errorf("하한이 적용돼야 한다: got %.3f, want 0.70", got)
	}

	// 하한 0 이면 종전대로 실측값
	cfg2 := TestConfig()
	cfg2.SatiationRiseP90Floor = 0
	w2 := &Worker{cfg: cfg2, store: store}
	got2, ok2 := w2.tankTopviewRiseP90("tank_02")
	if !ok2 || got2 <= 0.29 || got2 >= 0.31 {
		t.Errorf("하한 0 이면 실측 P90(0.3): got %.3f ok=%v", got2, ok2)
	}
}

// 계수가 하한보다 높으면 건드리지 않는다 (tank_01 은 영향 없어야 함).
func TestTankTopviewRiseP90_AboveFloorUnchanged(t *testing.T) {
	store := &peakGateStore{}
	for i := 0; i < 20; i++ {
		store.agitation = append(store.agitation,
			`{"tank_id":"tank_01","camera_id":"cam_top_7_1_2_L","baseline":10,"peak":19}`) // rise 0.9
	}
	cfg := TestConfig()
	cfg.SatiationRiseP90Floor = 0.70
	w := &Worker{cfg: cfg, store: store}
	got, ok := w.tankTopviewRiseP90("tank_01")
	if !ok || got <= 0.89 || got >= 0.91 {
		t.Errorf("하한 위 값은 그대로여야 한다: got %.3f ok=%v", got, ok)
	}
}

// 마지막 펄스 판정 — 보상 상한을 다 써도 trailing gap 판정은 수행한다 (2026-08-01).
// 종전에는 상한 소진이 조건에 있어 판정 자체를 건너뛰었고, 실측에서 그렇게 누락된
// 마지막 펄스 2건이 둘 다 실제 미배출이었다(게이트 가동 후 94펄스 표본).
func TestLastPulseJudgeEligible_IgnoresCompensationBudget(t *testing.T) {
	cfg := TestConfig()
	cfg.CompensationMaxPerCycle = 2
	w := &Worker{
		cfg:          cfg,
		liveSeriesFn: func(string) ([]float64, time.Time, bool) { return nil, time.Time{}, false },
	}
	newCycle := func(granted int) *Cycle {
		c := NewFixedCycle("cycle_lastjudge", "tank_01", "", FixedParams{
			PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
		})
		c.SetOnoffFeeder(831, true)
		for i := 0; i < granted; i++ {
			c.grantCompensation(18.8)
		}
		return c
	}

	if !w.lastPulseJudgeEligible(newCycle(0)) {
		t.Error("보상 여유가 있으면 당연히 판정 대상")
	}
	// 핵심 — 상한을 다 써도 판정은 계속한다.
	if !w.lastPulseJudgeEligible(newCycle(2)) {
		t.Error("보상 상한 소진이어도 마지막 펄스는 판정해야 한다")
	}

	// 자율모드 off (보상 차단) 도 판정은 남긴다 (2026-08-14) — 종전에는 off 인 동안
	// 사이클마다 마지막 펄스 기록이 빠져 창 검증 표본과 정답지가 한 발씩 줄었다.
	// 실제 보상 발사는 maybeCompensatePulse 안에서 따로 차단된다.
	offMode := newCycle(0)
	offMode.autonomousMode = "off"
	if !w.lastPulseJudgeEligible(offMode) {
		t.Error("보상이 차단돼 있어도 마지막 펄스는 판정해야 한다")
	}

	// 판정 자체가 불가능한 조건은 종전대로 제외된다.
	noSeries := &Worker{cfg: cfg}
	if noSeries.lastPulseJudgeEligible(newCycle(0)) {
		t.Error("라이브 시리즈가 없으면 판정 불가")
	}
	offCfg := TestConfig()
	offCfg.CompensationMaxPerCycle = 0
	wOff := &Worker{cfg: offCfg, liveSeriesFn: w.liveSeriesFn}
	if wOff.lastPulseJudgeEligible(newCycle(0)) {
		t.Error("보상 기능 자체가 꺼져 있으면 종전대로 제외")
	}
}
