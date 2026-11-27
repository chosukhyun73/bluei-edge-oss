package feed_cycle

// 사이클 반응 지수 (clip index) — 2026-08-06 운영자 설계.
//
// 목적: 사이클이 끝난 뒤 "이번 급이에 어체가 얼마나 반응했나"를 두 숫자로 요약해,
// **다음 사이클의 공급량·시점** 판단 근거로 쓴다. 사이클 안에서 공급이 되고 있는지
// (배출 판정)와 언제 멈출지(중단 판정)는 라이브 곡선이 맡고, 이 지수는 그와 역할이
// 다르다 — 라이브는 이번 사이클, 지수는 다음 사이클을 본다.
//
//	강도(level)      배출된 펄스들의 r3 점수 중앙값 — "얼마나 배고팠나"
//	방향(direction)  후반 평균 / 전반 평균          — "얼마나 배불러졌나"
//
// ⚠ 2026-08-08 부터 요동 곡선이 아니라 **r3 점수**를 쓴다. 급이 반응은 탑뷰 전용이고
// r3(LRCN 2-head feeding head)가 바로 그 질문에 답하도록 학습된 모델이다 — 요동에서
// 뽑던 것은 학습된 도구를 두고 쓰던 대용품이었다. 근거는 clipIndexBlock 주석 참조.
//
// ⚠ 강도는 수조마다 절대 스케일이 다르므로(r3 실측 중앙 tank_01 0.689 / tank_02 0.426)
// 수조별 기준선을 config 로 받는다. 방향은 비율이라 스케일이 상쇄돼 공통 기준을 쓴다
// (r3 실측 중앙 0.976).

import (
	"context"
	"sort"
	"strconv"
	"time"
)

// 상태 코드. 표시 문구는 message 로 함께 싣는다.
const (
	clipStateSustained = "sustained" // 아직 활동이 지속됩니다
	clipStateNormal    = "normal"    // 활동이 정상적으로 감소했습니다
	clipStateDrop      = "drop"      // 활동이 급격히 하락했습니다
	clipStateLow       = "low"       // 활동이 너무 낮습니다
)

// clipIndexMinPulses — 배출 펄스가 이보다 적으면 추세를 못 읽는다.
const clipIndexMinPulses = 4

// dischargeVerdicts — discharge_check 블록에서 per_pulse 를 꺼낸다. 블록이 없거나
// 형태가 다르면 빈 슬라이스(제외 없이 전 펄스 사용).
func dischargeVerdictsOf(dischargeCheck map[string]any) []string {
	raw, _ := dischargeCheck["per_pulse"].([]string)
	return raw
}

// clipIndexScoreTimeout — 보고를 막을 수 있는 상한. r3 추론 실측 292건에서 평균
// 9.5s · 최대 18.1s 라 1.7배 여유다. 넘으면 clip_index 만 비고 보고는 그대로 나간다.
const clipIndexScoreTimeout = 30 * time.Second

// clipIndexBlock — 보고 payload 의 clip_index 블록. 계산 불가면 nil(보고는 계속).
//
// scores 는 r3(LRCN 2-head feeding head)의 펄스별 점수다. 요동 곡선이 아니라 이걸
// 쓰는 이유는 **r3 가 이 질문에 답하도록 학습된 유일한 모델**이기 때문이다
// (2026-08-08 운영자 결정). 실측 근거는 운영자 라벨 94사이클 대조:
//
//	강도 ↔ 운영자 평균   요동 +0.388  →  r3 +0.514
//	방향 ↔ 운영자 환산   요동 +0.285  →  r3 +0.268   (동률, 병목은 제외 규칙)
//
// 덤으로 기저 오염이 사라진다. 요동 상승폭은 `max(반응창) - 기저` 라 기저가 튀면
// 값이 붕괴했다(201사이클 r=-0.68, 08-07 16:30 실사례 강도 2.36 → 0.05).
// r3 는 기저를 빼지 않으므로 이 경로 자체가 없다.
//
// verdicts 는 같은 보고의 discharge_check.per_pulse — "no" 펄스는 사료가 안 나간
// 것이므로 반응 지수에서 뺀다. 넣으면 반응이 없는 게 당연한 펄스가 하강으로 잡혀
// 배부른 것으로 오독된다.
func (rp *Reporter) clipIndexBlock(ctx context.Context, cycleID, tankID string,
	scores map[int]float64, verdicts []string) map[string]any {
	if rp.clipIndexDirLow <= 0 || rp.clipIndexDirHigh <= 0 {
		return nil // 미설정 — 임계 없이는 상태를 못 매긴다
	}
	if len(scores) < clipIndexMinPulses {
		return nil // r3 미채점(LRCN 미기동·타임아웃·클립 없음)
	}
	pulses := rp.collectPulseTimings(ctx, cycleID)
	if len(pulses) < clipIndexMinPulses {
		return nil
	}
	sort.Slice(pulses, func(i, j int) bool { return pulses[i].PulseNo < pulses[j].PulseNo })

	vals := make([]float64, 0, len(pulses))
	excluded := 0
	for i, p := range pulses {
		if i < len(verdicts) && verdicts[i] == "no" {
			excluded++
			continue
		}
		v, ok := scores[p.PulseNo]
		if !ok {
			continue
		}
		vals = append(vals, v)
	}
	if len(vals) < clipIndexMinPulses {
		return nil
	}

	level := median(vals)
	k := len(vals) / 3
	if k < 2 {
		k = 2
	}
	head := meanOf(vals[:k])
	if head <= 0 {
		return nil
	}
	direction := meanOf(vals[len(vals)-k:]) / head

	state, message := rp.clipState(tankID, level, direction)
	return map[string]any{
		"level":           roundTo2(level),
		"direction":       roundTo2(direction),
		"state":           state,
		"message":         message,
		"pulses_used":     len(vals),
		"pulses_excluded": excluded,
		"source":          "r3",
		"recommendation":  rp.clipRecommendation(tankID, level, direction),
	}
}

// 권고 배율 — 표기 전용이므로 config 로 빼지 않는다. 운영자 검토 후 실제 적용 단계에서
// 옮긴다. 대칭 ±10% 로 둔 것은 한쪽으로만 깎이는 래칫을 만들지 않기 위해서다.
const (
	clipRecoAmountUp     = 1.10
	clipRecoAmountDown   = 0.90
	clipRecoIntervalUp   = 1.10 // 간격을 늘린다 = 다음 급이를 늦춘다
	clipRecoIntervalDown = 0.90
)

// clipRecommendation — 이 사이클의 지수로 본 **다음 사이클** 권고.
//
// ⚠ 표기 전용이다. 급이 worker 는 이 값을 읽지 않는다 (2026-08-07 운영자 지시:
// "실제로 활용은 지금 안 할 거지만 일단 표기를 해서 알려달라").
//
//	양(amount)     ← 방향. 끝까지 반응이 남았으면 부족했던 것이니 늘리고,
//	                 일찍 꺾였으면 충분했던 것이니 줄인다.
//	간격(interval) ← 강도. 평소만큼 배고프지 않았으면 아직 소화 중이므로 늦추고,
//	                 평소보다 배고팠으면 당긴다.
//
// 두 축을 상태(state)에서 뽑지 않고 원값에서 따로 뽑는다 — 상태는 강도가 방향을
// 덮어쓰는 우선순위가 있어 한쪽 정보가 사라진다.
//
// 임계는 실측 분위수다(192사이클, 7-23~8-07). 각 축이 25/50/25 로 갈린다.
func (rp *Reporter) clipRecommendation(tankID string, level, direction float64) map[string]any {
	amount, amountWhy := 1.0, "통상 감소 — 유지"
	switch {
	case direction >= rp.clipIndexDirHigh:
		amount, amountWhy = clipRecoAmountUp, "끝까지 반응이 남았다 — 부족했을 수 있다"
	case direction < rp.clipIndexDirLow:
		amount, amountWhy = clipRecoAmountDown, "일찍 꺾였다 — 충분했을 수 있다"
	}

	interval, intervalWhy := 1.0, "평소 수준 — 유지"
	p25, p75 := rp.clipIndexLevelP25[tankID], rp.clipIndexLevelP75[tankID]
	switch {
	case p75 > 0 && level > p75:
		interval, intervalWhy = clipRecoIntervalDown, "평소보다 배고팠다 — 당겨도 된다"
	case p25 > 0 && level < p25:
		interval, intervalWhy = clipRecoIntervalUp, "평소만큼 배고프지 않았다 — 늦춰도 된다"
	}

	return map[string]any{
		"applied":         false, // 급이에 반영되지 않는다
		"amount_factor":   amount,
		"amount_reason":   amountWhy,
		"interval_factor": interval,
		"interval_reason": intervalWhy,
		"summary":         clipRecoSummary(amount, interval),
	}
}

// clipRecoSummary — 한 줄 표기. "다음 사이클: 양 +10% · 간격 유지" 꼴.
func clipRecoSummary(amount, interval float64) string {
	return "다음 사이클: 양 " + pctDelta(amount) + " · 간격 " + pctDelta(interval)
}

func pctDelta(f float64) string {
	switch {
	case f > 1:
		return "+" + strconv.Itoa(int((f-1)*100+0.5)) + "%"
	case f < 1:
		return "-" + strconv.Itoa(int((1-f)*100+0.5)) + "%"
	default:
		return "유지"
	}
}

// clipState — 강도·방향을 상태로 옮긴다. 강도가 기준선 아래면 방향과 무관하게
// "낮음"이다 — 애초에 적게 먹은 사이클에서 하강 여부는 의미가 없다.
//
// ⚠ 문구는 "너무 낮습니다"가 아니라 "평소보다 낮습니다"다 (2026-08-07 운영자 확정).
// 임계가 P25 이므로 이 상태는 정의상 4사이클에 1번 뜬다 — 이상 신호가 아니라 하위
// 4분위라는 위치 표시다. 실측으로도 시간대별 발동률 8~43% 로 흩어져 있고, 운영자가
// 첫 발동(08-07 10:30 tank_01, 강도 2.40)을 "적당한데 낮지 않다"고 판정했다.
func (rp *Reporter) clipState(tankID string, level, direction float64) (string, string) {
	if p25 := rp.clipIndexLevelP25[tankID]; p25 > 0 && level < p25 {
		return clipStateLow, "활동이 평소보다 낮습니다"
	}
	switch {
	case direction >= rp.clipIndexDirHigh:
		return clipStateSustained, "아직 활동이 지속됩니다"
	case direction < rp.clipIndexDirLow:
		return clipStateDrop, "활동이 급격히 하락했습니다"
	default:
		return clipStateNormal, "활동이 정상적으로 감소했습니다"
	}
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
