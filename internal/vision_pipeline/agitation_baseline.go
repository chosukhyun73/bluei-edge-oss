package vision_pipeline

// baseline(비급이 상시) 클립의 평소 활동성 — 클립당 요동 대표값 1점 적재.
//
// 급이 요동곡선(vision.agitation.recorded)은 클립 초반(급이 전)을 기저로 쓰는
// 사이클 내 상대 곡선이지만, 7~9초 baseline 클립은 클립 자체가 기저라 그 공식이
// 무의미하다. 여기서는 원시 요동 대표값(mean/peak)만 적재하고, 정규화(카메라별
// 롤링 기저 대비)는 소비처(추론 모니터링)가 수행한다. 시리즈는 저장하지 않는다
// (~2,300클립/일 — 대표값이면 충분, 이벤트 볼륨 최소화).

import (
	"context"
	"os"
	"strings"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/common"
)

// EventAgitationBaselineRecorded — 평소 활동성(요동 대표값) 이벤트 타입.
const EventAgitationBaselineRecorded = "vision.agitation.baseline.recorded"

// OnBaselineClip — 상시(baseline) 클립의 요동 대표값 계산 + 이벤트 적재.
// capture onResult 에서 phase=baseline 클립에 대해 호출. LRCN 과 무관하며
// 캡처 루프를 막지 않도록 goroutine 처리 (~0.7s/클립).
func (p *Pipeline) OnBaselineClip(r *capture.Result) {
	if p == nil || r == nil || p.sink == nil {
		return
	}
	go p.recordBaselineAgitation(r)
}

func (p *Pipeline) recordBaselineAgitation(r *capture.Result) {
	p.appendBaselineAgitation(r, false)
}

// nightMinLuma — 야간(IR) 클립 활동성 측정 최소 휘도. 이보다 어두우면 진짜
// 암흑(IR 조명 없음/렌즈 가림)으로 보고 측정을 스킵한다 — 프레임 차분 ~0 이
// "활동성 0" 으로 오독되는 것을 막는다.
const nightMinLuma = 10.0

// OnNightClip — dark-sleep 폐기 대상(소등/IR) 클립의 야간 활동성 측정.
// 소등 시 CCTV 는 IR 흑백으로 전환되어 프레임 차분(요동)은 여전히 유효하다.
// 측정 후 mp4/jpg 삭제까지 담당 (capture 의 삭제 책임 인수 — 학습 풀 미적재 유지).
func (p *Pipeline) OnNightClip(r *capture.Result) {
	if p == nil || r == nil {
		return
	}
	go p.recordNightAgitation(r)
}

func (p *Pipeline) recordNightAgitation(r *capture.Result) {
	defer func() {
		_ = os.Remove(r.MP4Path)
		_ = os.Remove(strings.TrimSuffix(r.MP4Path, ".mp4") + ".jpg")
	}()
	if p.sink == nil {
		return
	}
	// 진짜 암흑 판정 — 포스터 프레임 휘도 (IR 흑백은 밝게 나온다).
	if jpg, err := os.ReadFile(strings.TrimSuffix(r.MP4Path, ".mp4") + ".jpg"); err == nil {
		if luma, lerr := meanLuma(jpg); lerr == nil && luma < nightMinLuma {
			p.log.Debug("night clip too dark — skip agitation",
				"clip_id", r.ClipID, "camera_id", r.CameraID, "luma", luma)
			return
		}
	}
	p.appendBaselineAgitation(r, true)
}

func (p *Pipeline) appendBaselineAgitation(r *capture.Result, night bool) {
	log := p.log.With("clip_id", r.ClipID, "tank_id", r.TankID, "camera_id", r.CameraID)

	m, err := runAgitationScript(r.MP4Path)
	if err != nil {
		log.Warn("baseline agitation script failed", "error", err, "night", night)
		return
	}
	mean := 0.0
	for _, v := range m.Series {
		mean += v
	}
	mean /= float64(len(m.Series)) // parseAgitationOutput 이 len>0 보장

	payload := map[string]any{
		"clip_id":     r.ClipID,
		"tank_id":     r.TankID,
		"camera_id":   r.CameraID,
		"captured_at": common.FormatTime(r.CapturedAt),
		"n_seconds":   m.NSeconds,
		"mean":        mean,
		"peak":        m.Peak,
		"night":       night, // 야간 IR 은 주간과 스케일이 달라 소비처가 주/야 분리 정규화
	}
	if _, err := p.sink.AppendEvent(context.Background(), "g3_pipeline", "vision", r.CameraID,
		EventAgitationBaselineRecorded, r.ClipID, payload); err != nil {
		log.Warn("append baseline agitation event failed", "error", err)
		return
	}
	// 클립 ~3분(주)/~5분(야) 간격 × 전 카메라 — 성공 로그는 Debug (Info 는 로그 소음).
	log.Debug("baseline agitation recorded", "mean", mean, "peak", m.Peak, "night", night)
}
