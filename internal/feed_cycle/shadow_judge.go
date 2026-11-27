package feed_cycle

// 섀도 멈춤 판정 (축1 1b — docs/wip/autonomous-feeding-roadmap-2026-07-06.md).
//
// 사이클 보고 시점에 펄스별 요동 prior 를 산출해 판정 룰 v0 를 적용하고
// "AI였다면 pN에서 중단"을 feed.cycle.report 에 기록한다. 기록만 — 실제 중단은
// L2 승격(운영자 승인) 후 worker 배선에서. 판정 시점이 보고 시점이어도 결과는
// 실시간 판정과 동일하다(같은 series·같은 창·같은 룰).
//
// 신호 소스: vision.agitation.live(라이브 세션, L2 와 동일 신호) 우선,
// 없으면 클립 사후 vision.agitation.recorded 로 폴백(카메라별 펄스 max —
// scripts/replay_response_judge.py 와 동일 규약).

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

// agitationCurve — 판정에 쓰는 요동 곡선 (라이브/클립 공통 최소 필드).
type agitationCurve struct {
	Series   []float64
	Baseline float64
	StartAt  time.Time
	CameraID string
}

// gap끝 복귀도 (2026-07-07 실증 → 2026-07-08 병기 필드 자동화).
// 각 펄스 창의 마지막 windowSec 초(= 다음 펄스 직전) 평균 활동이 기저×factor 이내로
// 내려왔으면 복귀(만복 방향), 아니면 미복귀(섭이 지속 — 재펄스/증량 근거 후보).
// ⚠ 병기 기록만 — stop_at_pulse 판정에는 사용하지 않는다 (1주 일치율 측정 중 룰 불변).
const (
	gapEndRecoveryWindowSec = 5
	gapEndRecoveryFactor    = 1.2
)

// shadowJudgeBlock — 보고 payload 의 shadow_judge + discharge_check 블록.
// 판정 불가(펄스<2 또는 요동 데이터 없음)면 nil. 실패는 보고를 막지 않는다(warn 만).
func (rp *Reporter) shadowJudgeBlock(ctx context.Context, cycleID, tankID string, since time.Time, pulsesExecuted int) (map[string]any, map[string]any) {
	pulses := rp.collectPulseTimings(ctx, cycleID)
	if len(pulses) < 2 {
		return nil, nil
	}
	segs := DeriveSegments(pulses, 0)
	if len(segs) < 2 {
		return nil, nil
	}

	curves, source := rp.collectJudgeCurves(ctx, cycleID, since)
	if len(curves) == 0 {
		return nil, nil
	}

	// 펄스별 prior — 곡선이 여럿(클립 폴백, 다중 카메라)이면 펄스별 max.
	priors := make([]float64, 0, len(segs))
	for _, sg := range segs {
		best, n := 0.0, 0
		for _, c := range curves {
			startSec := int(sg.StartAbs.Sub(c.StartAt).Seconds())
			endSec := int(sg.EndAbs.Sub(c.StartAt).Seconds())
			p, _, _, cnt := vision_pipeline.SegmentPrior(c.Series, c.Baseline, startSec, endSec)
			if cnt > 0 {
				n += cnt
				if p > best {
					best = p
				}
			}
		}
		if n == 0 {
			return nil, rp.dischargeBlock(segs, curves) // 창이 곡선 밖 — 섀도 판정만 포기
		}
		priors = append(priors, math.Round(best*100)/100)
	}

	track, params := "T", vision_pipeline.TrackTParams()
	if rp.tankHasUnderwaterCamera(ctx, tankID) {
		track, params = "U", vision_pipeline.TrackUParams()
	}
	stopAt := vision_pipeline.JudgeStopPulse(priors, params)

	block := map[string]any{
		"rule":          "no_response_streak_v0",
		"track":         track,
		"priors":        priors,
		"stop_at_pulse": stopAt,
		"source":        source,
		"shadow":        true, // 기록만 — 실제 중단 아님
	}
	if stopAt > 0 && pulsesExecuted > stopAt {
		block["pulses_saved"] = pulsesExecuted - stopAt
	}
	if ratios, recovered, ok := gapEndRecovery(segs, curves); ok {
		block["gap_end_ratio"] = ratios       // 펄스별 gap끝 활동/기저 비
		block["gap_end_recovery"] = recovered // true = 기저×1.2 이내 복귀(만복 방향)
	}
	// v1 — 배출 확인과 반응 판정을 분리한 중단 판정 (shadow_stop.go). v0 는 두
	// 원인을 못 갈라 급이기 고장을 만복으로 읽는다. 둘 다 남겨 비교한다.
	if v1 := rp.shadowStopBlock(ctx, tankID, segs, curves); v1 != nil {
		block["v1"] = v1
	}
	return block, rp.dischargeBlock(segs, curves)
}

// gapEndRecovery — 펄스별 gap끝 복귀도. 각 펄스 창 [StartAbs,EndAbs)의 마지막
// gapEndRecoveryWindowSec 초 평균/기저 비를 곡선별로 구해 max 를 취한다
// (한 카메라라도 활동이 남아 있으면 미복귀 — priors 의 max 규약과 동일 방향).
// 어느 펄스든 곡선 창을 벗어나면 전체를 기록하지 않는다 (부분 배열은 해석 혼란).
func gapEndRecovery(segs []Segment, curves []agitationCurve) ([]float64, []bool, bool) {
	ratios := make([]float64, 0, len(segs))
	recovered := make([]bool, 0, len(segs))
	for _, sg := range segs {
		best, n := 0.0, 0
		for _, c := range curves {
			if c.Baseline <= 0 {
				continue
			}
			// 창 끝이 곡선 밖이면(라이브 관찰 꼬리가 마지막 펄스 창보다 짧은 경우 —
			// 07-08 13:00 tank_02 실측) 곡선 끝으로 클램프해 "관찰 가능한 가장 늦은
			// gap끝"을 쓴다. 창이 펄스 시작 이전까지 밀리면 이 곡선으론 판정 불가.
			endSec := int(sg.EndAbs.Sub(c.StartAt).Seconds())
			if L := len(c.Series); endSec > L {
				endSec = L
			}
			if endSec-gapEndRecoveryWindowSec < int(sg.StartAbs.Sub(c.StartAt).Seconds()) {
				continue
			}
			m, cnt := seriesMean(c.Series, endSec-gapEndRecoveryWindowSec, endSec)
			if cnt == 0 {
				continue
			}
			n += cnt
			if r := m / c.Baseline; r > best {
				best = r
			}
		}
		if n == 0 {
			return nil, nil, false
		}
		ratios = append(ratios, math.Round(best*100)/100)
		recovered = append(recovered, best <= gapEndRecoveryFactor)
	}
	return ratios, recovered, true
}

// seriesMean — series[startSec:endSec) 평균과 표본 수 (창은 series 범위로 클램프).
func seriesMean(series []float64, startSec, endSec int) (float64, int) {
	if startSec < 0 {
		startSec = 0
	}
	if endSec > len(series) {
		endSec = len(series)
	}
	if endSec <= startSec {
		return 0, 0
	}
	sum := 0.0
	for i := startSec; i < endSec; i++ {
		sum += series[i]
	}
	return sum / float64(endSec-startSec), endSec - startSec
}

// collectPulseTimings — 이 사이클의 feed.pulse.completed → PulseTiming 목록.
func (rp *Reporter) collectPulseTimings(ctx context.Context, cycleID string) []PulseTiming {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType:      events.EventFeedPulseCompleted,
		PayloadCycleID: cycleID,
		Limit:          200,
	})
	if err != nil {
		rp.log.Warn("shadow judge: query pulses failed", "cycle_id", cycleID, "error", err)
		return nil
	}
	var out []PulseTiming
	for _, e := range evs {
		var p struct {
			PulseNo          int    `json:"pulse_no"`
			ActualDurationMs int    `json:"actual_duration_ms"`
			CompletedAt      string `json:"completed_at"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		at, perr := time.Parse(time.RFC3339Nano, p.CompletedAt)
		if perr != nil {
			continue
		}
		out = append(out, PulseTiming{PulseNo: p.PulseNo, CompletedAt: at, ActualDurationMs: p.ActualDurationMs})
	}
	return out
}

// collectJudgeCurves — 라이브 요동 우선, 없으면 클립 사후 곡선들.
func (rp *Reporter) collectJudgeCurves(ctx context.Context, cycleID string, since time.Time) ([]agitationCurve, string) {
	type curvePayload struct {
		CycleID    string    `json:"cycle_id"`
		CameraID   string    `json:"camera_id"`
		Series     []float64 `json:"series"`
		Baseline   float64   `json:"baseline"`
		StartedAt  string    `json:"started_at"`  // live
		CapturedAt string    `json:"captured_at"` // clip
	}
	load := func(eventType string) []agitationCurve {
		evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
			EventType: eventType, Since: &since, Limit: 200,
		})
		if err != nil {
			rp.log.Warn("shadow judge: query curves failed", "event_type", eventType, "error", err)
			return nil
		}
		var out []agitationCurve
		for _, e := range evs {
			var p curvePayload
			if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.CycleID != cycleID || len(p.Series) == 0 {
				continue
			}
			startStr := p.StartedAt
			if startStr == "" {
				startStr = p.CapturedAt
			}
			at, perr := time.Parse(time.RFC3339Nano, startStr)
			if perr != nil {
				continue
			}
			out = append(out, agitationCurve{Series: p.Series, Baseline: p.Baseline, StartAt: at, CameraID: p.CameraID})
		}
		// 수중캠 곡선 제외(비수중 곡선이 하나라도 있으면) — 어체 근접 통과로 상승률이
		// 포화(prior 1.00 고정)돼 펄스별 max 를 항상 지배, 판정이 무의미해진다
		// (2026-07-07 09:30 tank_01 실측: priors 전부 1.00). vision_segments 의
		// 라벨링 prior 소스 규칙과 동일. 비수중이 없으면 수중이라도 그대로 사용.
		hasSurface := false
		for _, c := range out {
			if !strings.Contains(c.CameraID, "underwater") {
				hasSurface = true
				break
			}
		}
		if hasSurface {
			kept := out[:0]
			for _, c := range out {
				if !strings.Contains(c.CameraID, "underwater") {
					kept = append(kept, c)
				}
			}
			out = kept
		}
		return out
	}
	// 표면(비수중) 곡선 우선 — 라이브에 표면이 없으면(예: 라이브 세션이 수중캠만 연 경우)
	// 클립 사후 곡선의 표면을 쓴다. 표면이 어디에도 없으면 수중이라도 사용(폴백).
	hasSurface := func(cs []agitationCurve) bool {
		for _, c := range cs {
			if !strings.Contains(c.CameraID, "underwater") {
				return true
			}
		}
		return false
	}
	live := load(vision_pipeline.EventAgitationLive)
	if hasSurface(live) {
		return live, "live"
	}
	clip := load(vision_pipeline.EventAgitationRecorded)
	if hasSurface(clip) {
		return clip, "clip"
	}
	if len(live) > 0 {
		return live, "live_underwater_only"
	}
	if len(clip) > 0 {
		return clip, "clip_underwater_only"
	}
	return nil, ""
}

// tankHasUnderwaterCamera — 트랙 U/T 구분 (수중캠 보유 = U).
func (rp *Reporter) tankHasUnderwaterCamera(ctx context.Context, tankID string) bool {
	cams, err := rp.store.ListCameraProfiles(ctx, tankID)
	if err != nil {
		rp.log.Warn("shadow judge: camera list failed", "tank_id", tankID, "error", err)
		return false
	}
	for _, c := range cams {
		if strings.EqualFold(c.Position, "underwater") {
			return true
		}
	}
	return false
}
