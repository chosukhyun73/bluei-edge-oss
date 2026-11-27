package feed_cycle

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"
)

// 요동 국소상승 채널 — 착수창(run+23~33s) 상승이 있는 펄스만 배출로 판정.
func TestDischargeVerdicts(t *testing.T) {
	t0 := time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)
	seg := func(startS, endS int) Segment {
		return Segment{StartAbs: t0.Add(time.Duration(startS) * time.Second),
			EndAbs: t0.Add(time.Duration(endS) * time.Second)}
	}
	segs := []Segment{seg(0, 60), seg(60, 120), seg(120, 180)}

	// 기저 5.0, p1(25~29s)·p2(85~89s) 착수창에 상승 +15. p3 는 상승 없음(무배출).
	series := make([]float64, 200)
	for i := range series {
		series[i] = 5.0
	}
	for _, s := range [][2]int{{25, 29}, {85, 89}} {
		for i := s[0]; i < s[1]; i++ {
			series[i] = 20.0
		}
	}
	curves := []agitationCurve{{Series: series, Baseline: 5.0, StartAt: t0, CameraID: "cam_top"}}

	got := dischargeVerdicts(segs, curves)
	want := []string{"yes", "yes", "no"}
	if len(got) != len(want) {
		t.Fatalf("verdicts = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("p%d = %q, want %q (verdicts=%v)", i+1, got[i], want[i], got)
		}
	}

	block := (&Reporter{}).dischargeBlock(segs, curves)
	if block["suspected_no_discharge"] != 1 {
		t.Errorf("suspected_no_discharge = %v, want 1", block["suspected_no_discharge"])
	}

	// 곡선 없음 → nil (보고에 미기록).
	if (&Reporter{}).dischargeBlock(segs, nil) != nil {
		t.Error("no curves must yield nil block")
	}
}

// gap끝 복귀도 — 곡선이 마지막 펄스 창보다 짧아도(라이브 꼬리 짧음) 곡선 끝으로
// 클램프해 판정한다 (07-08 13:00 tank_02 실측 회귀).
func TestGapEndRecoveryClampsToCurveEnd(t *testing.T) {
	t0 := time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC)
	segs := []Segment{
		{StartAbs: t0, EndAbs: t0.Add(60 * time.Second)},
		{StartAbs: t0.Add(60 * time.Second), EndAbs: t0.Add(130 * time.Second)}, // 창 끝 130s > 곡선 100s
	}
	series := make([]float64, 100)
	for i := range series {
		series[i] = 5.0 // 기저 그대로 = 복귀
	}
	curves := []agitationCurve{{Series: series, Baseline: 5.0, StartAt: t0}}

	ratios, recovered, ok := gapEndRecovery(segs, curves)
	if !ok || len(ratios) != 2 {
		t.Fatalf("ok=%v ratios=%v (곡선 끝 클램프로 판정돼야 함)", ok, ratios)
	}
	if !recovered[0] || !recovered[1] {
		t.Errorf("recovered = %v, want all true (ratios=%v)", recovered, ratios)
	}
}

// 실사이클 회귀 — 2026-07-08 13:00 tank_02 (운영자 육안 + 오프라인 도구로
// p1·p2 배출 / p3~p5 무배출 확인). Go 포팅이 같은 판정을 내야 한다.
func TestDischargeVerdicts_RealCycleTank02(t *testing.T) {
	raw, err := os.ReadFile("testdata/discharge_tank02_20260708.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Curve struct {
			StartedAt string    `json:"started_at"`
			Baseline  float64   `json:"baseline"`
			Series    []float64 `json:"series"`
			CameraID  string    `json:"camera_id"`
		} `json:"curve"`
		Pulses []struct {
			PulseNo          int    `json:"pulse_no"`
			CompletedAt      string `json:"completed_at"`
			ActualDurationMs int    `json:"actual_duration_ms"`
		} `json:"pulses"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	startAt, _ := time.Parse(time.RFC3339Nano, fx.Curve.StartedAt)
	var pulses []PulseTiming
	for _, p := range fx.Pulses {
		at, _ := time.Parse(time.RFC3339Nano, p.CompletedAt)
		pulses = append(pulses, PulseTiming{PulseNo: p.PulseNo, CompletedAt: at, ActualDurationMs: p.ActualDurationMs})
	}
	segs := DeriveSegments(pulses, 0)
	curves := []agitationCurve{{Series: fx.Curve.Series, Baseline: fx.Curve.Baseline, StartAt: startAt, CameraID: fx.Curve.CameraID}}

	got := dischargeVerdicts(segs, curves)
	if len(got) != 5 {
		t.Fatalf("verdicts = %v", got)
	}
	// 확정 경계만 고정: p1·p2 = 배출(yes), p3~p5 = 배출 아님(no|unclear).
	// X↔~ 는 창 정렬 ±1s 에 따라 흔들리는 휴리스틱 경계라 고정하지 않는다
	// (오프라인 도구는 [O O X ~ X], 이 포팅은 [yes yes unclear no no] — 결론 동일).
	if got[0] != "yes" || got[1] != "yes" {
		t.Errorf("p1/p2 = %v/%v, want yes/yes (%v)", got[0], got[1], got)
	}
	for i := 2; i < 5; i++ {
		if got[i] == "yes" {
			t.Errorf("p%d = yes, want no|unclear (%v)", i+1, got)
		}
	}
	blk := (&Reporter{}).dischargeBlock(segs, curves)
	// 2026-07-09 창 재정의(기준선 run 이전 이동 — 조건반사 오염 제거) 후 no↔unclear
	// 경계가 이동해 확정 no 카운트가 2→1 로 감소. yes/비yes 경계(위 단언)는 불변.
	// 임계 재보정은 7-09 운영자 현장 ground truth 축적 후 (인수인계 참고).
	if n := blk["suspected_no_discharge"].(int); n < 1 {
		t.Errorf("suspected_no_discharge = %d, want >= 1", n)
	}
}

// 사후 채널 맥락 게이트 (2026-08-01) — 라이브와 같은 보정이 걸려야 두 채널이
// 같은 사이클을 다르게 기록하지 않는다.
func TestDischargeBlock_ContextGate(t *testing.T) {
	t0 := time.Date(2026, 8, 1, 4, 30, 0, 0, time.UTC)
	period := 70
	// 6펄스. p6 만 상승폭이 안 나오게 만든다.
	segs := make([]Segment, 0, 6)
	for i := 0; i < 6; i++ {
		st := t0.Add(time.Duration(i*period) * time.Second)
		segs = append(segs, Segment{
			Index: i, PulseNo: i + 1,
			StartAbs: st, EndAbs: st.Add(time.Duration(period) * time.Second),
		})
	}
	// 초반 펄스는 높고(12), 뒤로 갈수록 내려가되 p6 반응창은 여전히 출렁인다
	// = "만복이라 상승폭은 없지만 어체는 움직인다" → 맥락상 배출.
	n := 6*period + 60
	series := make([]float64, n)
	for i := range series {
		base := 10.0
		if i < 250 {
			base = 12.0
		}
		series[i] = base + 0.02*math.Sin(float64(i)/3.0)
	}
	// p1~p5 는 착수 반응(상승)이 뚜렷 → yes
	for k := 0; k < 5; k++ {
		r := k * period
		for i := r + 10; i < r+25 && i < n; i++ {
			series[i] += 4.0
		}
	}
	// p6 만 상승 없이 반응창이 출렁 → 상승폭은 no, 맥락은 배출
	r6 := 5 * period
	for i := r6 + 25; i < r6+55 && i < n; i++ {
		series[i] = 10.0 + math.Sin(float64(i)/2.0)
	}
	curves := []agitationCurve{{Series: series, Baseline: 12.0, StartAt: t0, CameraID: "cam_top"}}

	// 게이트 OFF — 종전대로 p6 는 no 로 남는다.
	off := (&Reporter{}).dischargeBlock(segs, curves)
	offPulses := off["per_pulse"].([]string)
	if offPulses[5] != "no" {
		t.Fatalf("게이트 off 면 p6 는 no 여야 한다: %v", offPulses)
	}
	if _, has := off["raw_per_pulse"]; has {
		t.Error("게이트 off 면 raw_per_pulse 를 남기지 않는다")
	}

	// 게이트 ON — 맥락상 배출로 보여 unclear 로 완화되고 원본은 보존된다.
	rp := &Reporter{}
	rp.SetCompensationGate(true, 0, 0, 0)
	on := rp.dischargeBlock(segs, curves)
	onPulses := on["per_pulse"].([]string)
	if onPulses[5] != "unclear" {
		t.Errorf("게이트 on 이면 p6 는 unclear 여야 한다: %v", onPulses)
	}
	if on["context_softened"] != 1 {
		t.Errorf("context_softened = %v, want 1", on["context_softened"])
	}
	rawP := on["raw_per_pulse"].([]string)
	if rawP[5] != "no" {
		t.Errorf("raw_per_pulse 에 상승폭 원본(no)이 남아야 한다: %v", rawP)
	}
	if on["suspected_no_discharge"] != 0 {
		t.Errorf("완화 후 의심 발수 = %v, want 0", on["suspected_no_discharge"])
	}
}

// 만복 전제조건 (2026-08-02) — 배출(yes)이 충분히 확인되지 않은 사이클에서는
// 게이트를 걸지 않는다. 위 시나리오는 p1~p5 가 yes 라 confirmed=5.
// minConfirmed 를 6 으로 올리면 p6 시점에 전제 미충족 → 완화되지 않아야 한다.
func TestDischargeBlockGateSkippedWhenSatiationImpossible(t *testing.T) {
	t0 := time.Date(2026, 8, 2, 9, 30, 0, 0, time.UTC)
	const period, n = 70, 480
	segs := make([]Segment, 6)
	for k := range segs {
		segs[k] = Segment{PulseNo: k + 1, StartAbs: t0.Add(time.Duration(k*period) * time.Second)}
	}
	series := make([]float64, n)
	for i := range series {
		base := 10.0
		if i < 250 {
			base = 12.0
		}
		series[i] = base + 0.02*math.Sin(float64(i)/3.0)
	}
	for k := 0; k < 5; k++ {
		r := k * period
		for i := r + 10; i < r+25 && i < n; i++ {
			series[i] += 4.0
		}
	}
	r6 := 5 * period
	for i := r6 + 25; i < r6+55 && i < n; i++ {
		series[i] = 10.0 + math.Sin(float64(i)/2.0)
	}
	curves := []agitationCurve{{Series: series, Baseline: 12.0, StartAt: t0, CameraID: "cam_top"}}

	// 전제 충족(배출 5발 ≥ 3) → 종전대로 완화된다.
	met := &Reporter{}
	met.SetCompensationGate(true, 0, 0, 3)
	onMet := met.dischargeBlock(segs, curves)
	if got := onMet["per_pulse"].([]string)[5]; got != "unclear" {
		t.Errorf("전제 충족 시 p6 = %q, want unclear", got)
	}

	// 전제 미충족(배출 5발 < 6) → 게이트를 건너뛰고 no 가 유지된다.
	unmet := &Reporter{}
	unmet.SetCompensationGate(true, 0, 0, 6)
	onUnmet := unmet.dischargeBlock(segs, curves)
	if got := onUnmet["per_pulse"].([]string)[5]; got != "no" {
		t.Errorf("전제 미충족 시 p6 = %q, want no (게이트 미적용)", got)
	}
	if onUnmet["gate_skipped_no_satiation"] != 1 {
		t.Errorf("gate_skipped_no_satiation = %v, want 1", onUnmet["gate_skipped_no_satiation"])
	}
	if _, has := onUnmet["context_softened"]; has {
		t.Error("게이트를 건너뛰었으면 context_softened 를 남기지 않는다")
	}
}

// 급이량 정정 (2026-08-02) — 무배출 판정 펄스는 보상 발사 여부와 무관하게
// 급이량에서 빠져야 한다. 보상 상한이 소진돼도 장부는 사실을 기록한다.
func TestDeductPhantomOnlyDoesNotGrantPulse(t *testing.T) {
	c := &Cycle{Mode: ModeFixed, fixed: &FixedParams{TotalPulses: 6}}
	for i := 0; i < 6; i++ {
		c.recordPulseResult(18.8, 18.8)
	}
	if got := c.TotalAmountG(); got != 112.8 {
		t.Fatalf("6발 합산 = %v, want 112.8", got)
	}
	base := c.maxPulses()

	// 보상 승인 — 차감 + 펄스 허용치 +1
	c.grantCompensation(18.8)
	if got := c.TotalAmountG(); got != 94.0 {
		t.Errorf("보상 승인 후 급이량 = %v, want 94.0", got)
	}
	if c.maxPulses() != base+1 {
		t.Errorf("보상 승인은 펄스 허용치를 +1 해야 한다: %d → %d", base, c.maxPulses())
	}

	// 보상 미발사 — 차감만, 펄스 허용치는 그대로
	before := c.maxPulses()
	c.deductPhantomOnly(18.8)
	if got := c.TotalAmountG(); got != 75.2 {
		t.Errorf("차감 전용 후 급이량 = %v, want 75.2", got)
	}
	if c.maxPulses() != before {
		t.Errorf("차감 전용은 펄스 허용치를 바꾸면 안 된다: %d → %d", before, c.maxPulses())
	}
	if c.CompensationsGranted() != 1 {
		t.Errorf("보상 승인 수 = %d, want 1", c.CompensationsGranted())
	}
}
