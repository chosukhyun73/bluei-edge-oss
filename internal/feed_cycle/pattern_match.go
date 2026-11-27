package feed_cycle

// 사이클 통째 배출 판정 — 펄스 열과 요동 봉우리 열의 일치도 (2026-07-29 운영자 지시:
// "봉우리가 펄스에 움직이는 전체적인 내용을 평가해야지 하나하나가 아니라").
//
// 배경: 펄스별 국소상승(discharge_check)은 사이클 후반에 직전 반응이 가라앉지 않아
// 포화하면 실제로 배출됐는데도 no 로 떨어진다 — 2026-07-29 육안 대조로 3사이클 연속
// 실증(09:30 tank_01 p6~p8 / 10:00 tank_02 p5~p6 / 11:00 tank_02 p6~p8 전부 거짓 no).
// 펄스 하나만 보면 신호가 잡음에 묻히지만, 펄스 전체를 겹치면 잡음이 상쇄되고 급이
// 패턴만 남는다.
//
// 방법: 펄스마다 [착수 지연 → 정점 → 감쇠] 모양의 기대 반응을 더해 템플릿을 만들고,
// 실제 요동 곡선과 피어슨 상관을 구한다. 배출이 있으면 봉우리가 펄스에 물려 양의
// 상관이 나오고, 전량 무배출이면 상관이 사라진다.
//
// ⚠ 이 값은 기록·경보 전용이다. 급이 실행·보상 판정에는 쓰지 않는다.

import (
	"context"
	"math"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 기대 반응 템플릿 파라미터 — discharge_check 의 반응창(run+10~20s)과 같은 관측
// 근거를 쓰되, 창을 자르는 대신 연속 함수로 둔다.
const (
	patternRespStartS = 10   // 착수 시작 (명령 → 리드 → 수면 도달)
	patternPeakS      = 15   // 반응 정점
	patternRiseTauS   = 4.0  // 정점 이전 상승 시정수
	patternDecayTauS  = 25.0 // 정점 이후 감쇠 시정수
	patternSpanS      = 70   // 한 펄스의 기대 반응 지속 구간
	patternMinPulses  = 3    // 이보다 적으면 패턴이랄 게 없음
	patternMinSamples = 60   // 곡선이 이보다 짧으면 판정 불가

	// patternMatchWeak — 이 값 미만이면 "사이클 전량 무배출 의심".
	// ⚠ 잠정값이다. 진짜 무배출 사이클 표본이 1건뿐이다 (2026-07-28 16:30 tank_01,
	// 급이기 물리 고장, 일치도 -0.055). 같은 4일치에서 전량배출 32사이클의 일치도
	// 중앙은 +0.279 였고 이 임계 미만은 1건(3%)뿐이었다. 표본이 쌓이면 재보정할 것.
	patternMatchWeak = 0.0
)

// pulsePatternMatch — 펄스 열 대비 요동 곡선의 패턴 일치도.
// 곡선이 여럿이면 최댓값(한 카메라라도 급이 패턴을 봤으면 인정 — dischargeVerdicts 와 동일 규약).
// 판정 불가면 ok=false.
func pulsePatternMatch(segs []Segment, curves []agitationCurve) (float64, bool) {
	if len(segs) < patternMinPulses {
		return 0, false
	}
	best, found := 0.0, false
	for _, c := range curves {
		if len(c.Series) < patternMinSamples {
			continue
		}
		tpl := pulseResponseTemplate(segs, c)
		if tpl == nil {
			continue
		}
		r, ok := pearson(c.Series, tpl)
		if !ok {
			continue
		}
		if !found || r > best {
			best, found = r, true
		}
	}
	if !found {
		return 0, false
	}
	return math.Round(best*1000) / 1000, true
}

// pulseResponseTemplate — 각 펄스의 기대 반응을 더한 템플릿. 곡선 구간 안에 들어오는
// 펄스가 patternMinPulses 미만이면 nil.
func pulseResponseTemplate(segs []Segment, c agitationCurve) []float64 {
	tpl := make([]float64, len(c.Series))
	used := 0
	for _, sg := range segs {
		r := int(sg.StartAbs.Sub(c.StartAt).Seconds())
		if r+patternRespStartS >= len(tpl) || r+patternSpanS <= 0 {
			continue // 이 펄스는 곡선 밖
		}
		used++
		for t := r + patternRespStartS; t < r+patternSpanS && t < len(tpl); t++ {
			if t < 0 {
				continue
			}
			dt := float64(t - (r + patternPeakS))
			if dt >= 0 {
				tpl[t] += math.Exp(-dt / patternDecayTauS)
			} else {
				tpl[t] += math.Exp(dt / patternRiseTauS)
			}
		}
	}
	if used < patternMinPulses {
		return nil
	}
	return tpl
}

// pearson — 두 시계열의 피어슨 상관. 어느 쪽이든 분산이 0이면 ok=false.
func pearson(a, b []float64) (float64, bool) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < patternMinSamples {
		return 0, false
	}
	ma, mb := meanOf(a[:n]), meanOf(b[:n])
	var num, da, db float64
	for i := 0; i < n; i++ {
		x, y := a[i]-ma, b[i]-mb
		num += x * y
		da += x * x
		db += y * y
	}
	if da <= 0 || db <= 0 {
		return 0, false
	}
	return num / math.Sqrt(da*db), true
}

// patternWeakAlert — 일치도가 임계 미만이면 "사이클 통째 무배출 의심" 경보.
// 급이 실행에는 개입하지 않는다 — 운영자가 급이기를 점검할 판단 자료다.
func (rp *Reporter) patternWeakAlert(ctx context.Context, row *storage.FeedCycle, discharge map[string]any) {
	if discharge == nil {
		return
	}
	m, ok := discharge["pattern_match"].(float64)
	if !ok || m >= patternMatchWeak {
		return
	}
	payload := map[string]any{
		"tank_id":            row.TankID,
		"cycle_id":           row.CycleID,
		"pattern_match":      m,
		"threshold":          patternMatchWeak,
		"pulses_executed":    row.PulsesExecuted,
		"termination_reason": row.TerminationReason,
	}
	if _, err := rp.sink.AppendEvent(ctx, "feed_cycle", "", row.TankID,
		events.EventFeedCyclePatternWeak, row.CycleID, payload); err == nil {
		rp.log.Warn("cycle pattern weak — 통째 무배출 의심 (급이기 점검 필요)",
			"tank_id", row.TankID, "cycle_id", row.CycleID, "pattern_match", m)
	}
}
