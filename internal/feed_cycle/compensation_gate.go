package feed_cycle

// 보상 발사 정밀 게이트 (2026-07-29~30 운영자 지시: "차트의 움직임으로도 충분히 판단할 수 있잖어").
//
// 문제: 요동 상승폭 판정(judgeLivePulseDischarge)은 사이클 후반에 직전 반응이 가라앉지
// 않아 포화하면 정상 배출도 "no" 로 떨어뜨린다. 실측 no 비율이 펄스 순서에 따라
// p1 약 10% → p6 약 70% → p7 약 90% 로 치솟는데, 뒤쪽은 무배출이 아니라 어체가 배불러
// 반응이 없는 것이다(2026-07-29 육안 대조 확정). 그 "거짓 no" 마다 보상이 발사돼
// 과급이가 된다 — 라벨 253펄스에서 보상 49건 중 37건이 불필요했다.
//
// 관찰: 진짜 무배출 펄스는 두 가지를 동시에 보인다.
//   ① 요동 수준이 사이클 초반 대비 뚜렷이 내려앉는다 (자기기준비 낮음)
//   ② 반응창 안에서 거의 미동이 없다 (변동성 낮음)
// 반대로 만복에 의한 거짓 no 는 수준이 초반 대비 유지되거나(어체가 여전히 수면에 몰려
// 있음) 변동이 남아 있다. 2026-07-29 11:30 tank_01 사례가 전형: 실제 무배출인 p6 만
// 자기기준비 0.73(전 표본 최저)이고, 같은 사이클의 거짓 no p7 은 0.89 였다.
//
// 두 값 모두 **그 사이클 자신**을 기준으로 하므로 수조·카메라 진폭 차이를 흡수한다
// (tank_02 는 빛번짐·수류로 진폭이 tank_01 의 1/3 — 절대 임계로는 분리 불가였다).
//
// ⚠ 이 게이트는 **발사만** 좁힌다. 판정(verdict)·기록·경보는 무변이므로 놓치는 정보가
// 없다. 계산 불가 시에는 종전대로 발사한다(fail-open — 굶기는 쪽으로 실패하지 않는다).
//
// ⚠ 사이클 통째 무배출은 원리상 못 잡는다(기준 구간도 같이 내려앉으므로). 그쪽은
// 사이클 단위 지표(pattern_match.go)가 담당한다.

import (
	"math"
	"time"

	"bluei.kr/edge/internal/vision_pipeline"
)

// shouldApplySatiationGate — 만복 완화(거짓 no 구조) 를 이 펄스에 적용할지.
//
//	rawVerdict=="no"     완화는 no 를 unclear 로 낮추는 것이므로 no 일 때만 의미가 있다
//	gateEnabled          config 스위치
//	satiationPossible    배출 확인 N발 이상 — 안 먹었으면 배부를 수 없다 (2026-08-02)
//	negativeRise         rise < 0 이면 완화하지 않는다 (2026-08-16 운영자 결정)
//
// 음수는 "펄스 후 반응창 최댓값이 펄스 직전 평균보다 낮다"는 뜻이라 사료가 물에 닿았다면
// 나오기 어렵다. 실측 미전달 3건이 모두 음수였다. 부호는 σ 비례 임계와 달리 자가 없어
// 사이클 오염에 흔들리지 않는다.
func shouldApplySatiationGate(rawVerdict string, gateEnabled, satiationPossible, negativeRise bool) bool {
	return rawVerdict == "no" && gateEnabled && satiationPossible && !negativeRise
}

const (
	// p1~p3 은 기준 구간을 만드는 펄스라 자기 기준으로 비교할 수 없다 → 게이트 미적용.
	// 실측 무배출 17건 중 13건이 p4 이후라 적용 범위 손실은 작다.
	compGateMinPulseNo = 4
	compGateRefPeriods = 3 // 기준 구간 = 초반 3주기

	// 반응창 — discharge_check 의 반응창(+10~20s)보다 넓게 잡아 gap 끝까지 포함한다.
	// 만복 구간의 "완만한 하강"과 무배출의 "미동 없음"을 가르려면 창이 길어야 한다.
	compGateWinStartS = 8
	compGateWinEndS   = 55

	compGateDefaultRelMax = 0.85 // 자기기준비 상한 (실측 무배출 0.73~0.99, 대부분 ≤0.88)
	compGateDefaultVarMax = 0.04 // 변동성 상한 (기저 대비)

	compGateMinRefSamples = 30
	compGateMinCurSamples = 20
)

// compGateResult — 게이트 판정 결과. Checked=false 면 판정 불가(호출부는 종전대로 발사).
type compGateResult struct {
	Checked  bool
	Pass     bool    // true = 진짜 무배출로 보임 → 보상 발사
	Rel      float64 // 자기기준비
	VarRatio float64 // 반응창 변동성 / 기저
}

// contextSaysDischarged — 사후 채널용. 저장 클립 곡선으로 같은 맥락 판정을 돌려
// "배출된 것으로 보인다"(= 게이트가 보상을 보류할 상황)면 true.
// 곡선이 여럿이면 하나라도 배출로 보이면 true (dischargeVerdicts 의 max 규약과 같은 방향).
// 판정 불가면 false — 사후 채널도 fail-open 으로 원본 no 를 유지한다.
func contextSaysDischarged(sg Segment, curves []agitationCurve, relMax, varMax float64) bool {
	for _, c := range curves {
		res := compensationGate(c.Series, c.StartAt, sg.StartAbs, sg.PulseNo, relMax, varMax)
		if res.Checked && !res.Pass {
			return true
		}
	}
	return false
}

// compensationGate — 라이브 요동 series 로 "이 no 가 진짜 무배출인가"를 재확인한다.
// pulseNo 는 판정 대상 펄스 순번(1-based).
func compensationGate(series []float64, sessionStart, pulseRunAt time.Time, pulseNo int,
	relMax, varMax float64) compGateResult {

	if pulseNo < compGateMinPulseNo {
		return compGateResult{}
	}
	if relMax <= 0 {
		relMax = compGateDefaultRelMax
	}
	if varMax <= 0 {
		varMax = compGateDefaultVarMax
	}
	m := vision_pipeline.ComputeAgitationMetrics(series, 8)
	if m == nil || m.Baseline <= 0 {
		return compGateResult{}
	}
	sm := m.Series
	r := int(pulseRunAt.Sub(sessionStart).Seconds())
	if r <= 0 {
		return compGateResult{}
	}
	// 실측 주기 — 펄스 시각 이력을 보관하지 않으므로 경과시간/펄스수로 환산한다.
	period := r / (pulseNo - 1)
	if period <= 0 {
		return compGateResult{}
	}
	refEnd := compGateRefPeriods * period
	if refEnd > r { // 기준 구간이 이번 펄스를 침범하면 판정 불가
		refEnd = r
	}
	ref := sliceRange(sm, compGateWinStartS, refEnd)
	cur := sliceRange(sm, r+compGateWinStartS, r+compGateWinEndS)
	if len(ref) < compGateMinRefSamples || len(cur) < compGateMinCurSamples {
		return compGateResult{}
	}
	refMean := meanOf(ref)
	if refMean <= 0 {
		return compGateResult{}
	}
	rel := meanOf(cur) / refMean
	varRatio := pstdev(cur) / m.Baseline
	return compGateResult{
		Checked:  true,
		Pass:     rel < relMax && varRatio < varMax,
		Rel:      math.Round(rel*1000) / 1000,
		VarRatio: math.Round(varRatio*1000) / 1000,
	}
}
