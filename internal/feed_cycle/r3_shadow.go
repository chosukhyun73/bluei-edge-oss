package feed_cycle

import (
	"context"
	"math"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/inference"
)

// r3 섀도 판정 (2026-07-22) — 배출 주신호를 요동→r3(LRCN 탑뷰)로 전환하기 위한 관찰 골격.
//
// 요동(무차별 픽셀차)은 거짓 yes(결식)·거짓 no(과급이) 양방향 오판이 실증됐고, r3 가 같은
// 펄스를 정확히 판정함이 확인됐다([[project_agitation_limit_r3_direction]]). 다만 r3 배출판정
// 임계가 미보정이라 바로 대체하지 않고, 매 펄스마다 판정클립을 캡처해 r3 점수를 별도 이벤트로
// 적재만 한다(급이 보상 결정은 요동 유지 = 무변). 요동 verdict(feed.pulse.discharge_judged)와
// (cycle_id, pulse_no)로 조인해 일치를 관찰하고 임계를 보정한 뒤 주신호화로 승격한다.

const (
	// 판정클립 tail — gap 종료 후 반응 잔여를 조금 더 담기 위한 여유(초).
	r3ShadowClipTailSec = 2
	// 판정클립 상한(초) — 파일/추론 부담 방어. 긴 gap 이어도 이 이상 녹화하지 않는다.
	r3ShadowClipMaxSec = 150
	// 판정클립 하한(초) — LRCN 시퀀스 표집에 필요한 최소 길이.
	r3ShadowClipMinSec = 8
)

// r3ShadowEligible — r3 섀도 판정을 돌릴 조건. 요동 판정(maybeCompensatePulse)과 같은
// 사이클 종류(onoff_feeder fixed)로 한정해 요동과 apples-to-apples 비교가 되게 한다.
func (w *Worker) r3ShadowEligible(c *Cycle) bool {
	return w.cfg.R3ShadowEnabled && w.segScorer != nil && w.captureWorker != nil &&
		c.Mode == ModeFixed && c.IsOnoffFeeder()
}

// r3ShadowClipDurationSec — gap 반응창을 담는 판정클립 길이(초). gap + tail 을 min/max 로 클램프.
func r3ShadowClipDurationSec(gapMs int) int {
	d := gapMs/1000 + r3ShadowClipTailSec
	if d > r3ShadowClipMaxSec {
		return r3ShadowClipMaxSec
	}
	if d < r3ShadowClipMinSec {
		return r3ShadowClipMinSec
	}
	return d
}

// r3ShadowThreshold — feeding_score verdict 컷. 섀도 전용(보상 미영향). 0 → 0.5.
func (w *Worker) r3ShadowThreshold() float64 {
	if th := w.cfg.R3ShadowThreshold; th > 0 {
		return th
	}
	return 0.5
}

// launchR3Shadow — 펄스 발사 직후(펄스 OFF) 호출. 조건 충족 시 판정클립 캡처+채점+적재를
// 백그라운드 goroutine 으로 띄운다(비블로킹). c 필드는 이 caller goroutine 에서만 읽고,
// goroutine 에는 값으로만 넘겨 레이스를 피한다.
func (w *Worker) launchR3Shadow(c *Cycle, pulseNo, gapMs int) {
	if !w.r3ShadowEligible(c) {
		return
	}
	go w.runR3Shadow(c.CycleID, c.TankID, pulseNo, r3ShadowClipDurationSec(gapMs), c.autonomousMode)
}

// runR3Shadow — 판정클립 durationSec 캡처(gap 반응창) → LRCN feeding head 단일 세그먼트
// 채점 → feed.pulse.r3_shadow 적재. 실패는 warn 만(급이 무영향).
func (w *Worker) runR3Shadow(cycleID, tankID string, pulseNo, durationSec int, autonomousMode string) {
	// 앱 종료와 무관히 클립 완결까지 살아야 하므로 background 파생. 캡처 duration + 채점 여유.
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(durationSec)*time.Second+3*time.Minute)
	defer cancel()

	clipPath, cameraID, err := w.captureWorker.CaptureJudgeClip(ctx, tankID, durationSec)
	if err != nil {
		w.log.Warn("r3 shadow: judge clip capture failed",
			"cycle_id", cycleID, "tank_id", tankID, "pulse_no", pulseNo, "error", err)
		return
	}

	// 단일 세그먼트 = 클립 전체(펄스 OFF ~ gap 끝). 클립 시작이 오프셋 0.
	resp, err := w.segScorer.SegmentScores(ctx, inference.LRCNSegmentScoresRequest{
		ClipPath: clipPath,
		TankID:   tankID,
		Segments: []inference.LRCNSegmentWindow{{
			PulseNo:       pulseNo,
			OffsetStartMs: 0,
			OffsetEndMs:   durationSec * 1000,
		}},
	})
	if err != nil {
		w.log.Warn("r3 shadow: segment scoring failed",
			"cycle_id", cycleID, "tank_id", tankID, "pulse_no", pulseNo, "error", err)
		return
	}
	if len(resp.Scores) == 0 || resp.Scores[0].FeedingScore == nil {
		w.log.Warn("r3 shadow: no feeding score returned",
			"cycle_id", cycleID, "tank_id", tankID, "pulse_no", pulseNo)
		return
	}

	score := *resp.Scores[0].FeedingScore
	th := w.r3ShadowThreshold()
	verdict := "yes"
	if score < th {
		verdict = "no"
	}

	w.emitEvent(ctx, "feed.pulse.r3_shadow", cycleID, tankID, map[string]any{
		"cycle_id":        cycleID,
		"tank_id":         tankID,
		"pulse_no":        pulseNo,
		"camera_id":       cameraID,
		"clip_path":       clipPath,
		"feeding_score":   math.Round(score*1000) / 1000,
		"r3_verdict":      verdict, // yes | no (섀도 — 보상 결정 미반영)
		"threshold":       th,
		"clip_dur_s":      durationSec,
		"model_version":   resp.ModelVersion,
		"autonomous_mode": autonomousMode,
		"judged_at":       common.FormatTime(common.NowUTC()),
	})
	w.log.Info("r3 shadow judged",
		"cycle_id", cycleID, "tank_id", tankID, "pulse_no", pulseNo,
		"feeding_score", score, "r3_verdict", verdict, "threshold", th)
}
