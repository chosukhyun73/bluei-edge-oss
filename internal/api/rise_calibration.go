package api

// 카메라별 요동 상승률 자기 기준 정규화 (2026-07-20 운영자 결정).
//
// 요동 점수·prior 는 전 카메라 공통 스케일(상승률 +100% 포화)을 써 왔는데,
// 카메라별 동적범위가 크게 달라(1주 실측: tank_01 L크롭 +20~250%+ / tank_02
// R크롭 +6~65% / 수중 +83%~) 저진폭 카메라는 점수가 구조적으로 눌린다.
// 해법 = 카메라 자기 분포 기준: 점수 = clamp(상승률 / 그 카메라 최근 클립
// 피크 상승률 P90, 0~1). 1.0 = "이 카메라의 최근 강반응 수준".
//
// 적용 범위는 표시·라벨링 prior 층만. 판정(discharge_live)·만복 게이트·
// 섀도 판정은 불변 — 급이량을 좌우하는 안전 경로라 섀도 재측정 후 별도 전환.

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

const (
	riseCalibWindow    = 500  // 스캔 상한 (7일 창 안에서)
	riseCalibDays      = 7    // 최근 7일 — 펄스 정책·사료 레짐 변화에 적응
	riseCalibMinSample = 10   // 카메라당 표본 미만이면 legacy 스케일 폴백
	riseCalibPct       = 0.90 // P90
)

// riseP90ByCamera — 최근 7일 급이 요동 이벤트에서 카메라별 클립 피크 상승률 P90.
// 표본 부족 카메라는 미포함 (소비처는 0 = 미확보로 legacy 폴백).
func (s *Server) riseP90ByCamera(ctx context.Context) map[string]float64 {
	since := time.Now().UTC().AddDate(0, 0, -riseCalibDays)
	evList, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded,
		Since:     &since,
		Limit:     riseCalibWindow,
	})
	if err != nil {
		return nil
	}
	rises := map[string][]float64{}
	for _, e := range evList {
		var p agitationEventPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.Baseline <= 0 {
			continue
		}
		rises[p.CameraID] = append(rises[p.CameraID],
			vision_pipeline.PeakRiseFrac(p.Baseline, p.Peak))
	}
	out := map[string]float64{}
	for cam, rs := range rises {
		if p90, ok := risePercentile(rs, riseCalibPct); ok {
			// 과증폭 방지 하한 — feed_cycle.satiation_rise_p90_floor 와 같은 값을 쓴다
			// (2026-08-01). P90 이 자기 7일 데이터라 활동이 낮아지면 계수도 낮아져
			// 정규화값이 되레 올라가는 되먹임이 있다(실측 tank_02: 0.885 → 0.454,
			// 증폭 1.13 → 2.20배). 여기 값은 평가 모달의 AI 프리필(제안 점수)을
			// 만들므로, 부풀려지면 운영자 라벨까지 편향된다.
			if floor := s.riseP90Floor(); floor > 0 && p90 < floor {
				p90 = floor
			}
			out[cam] = p90
		}
	}
	return out
}

// risePercentile — 정렬 후 pct 분위값. 표본<riseCalibMinSample 또는 값이 0 에
// 가까우면 ok=false (스케일로 못 씀).
func risePercentile(rises []float64, pct float64) (v float64, ok bool) {
	if len(rises) < riseCalibMinSample {
		return 0, false
	}
	sorted := append([]float64(nil), rises...)
	sort.Float64s(sorted)
	v = sorted[int(float64(len(sorted))*pct)]
	if v < 1e-6 {
		return 0, false
	}
	return v, true
}

// calibratedRiseScore — clamp(rise/p90, 0, 1). p90 미확보(<=0) 면 legacy
// clamp(rise, 0, 1) (기존 +100% 포화 스케일).
func calibratedRiseScore(rise, p90 float64) float64 {
	if p90 > 0 {
		rise = rise / p90
	}
	return math.Max(0, math.Min(1, rise))
}

// segmentPriorScore — 세그먼트 제안 점수(agitation_prior). 자를 두 단으로 고른다.
//
//	앵커 있음  clamp((peak − baseline) / 앵커, 0, 1)   ← 절대 상승폭 기준
//	앵커 없음  종전 clamp(상승률 / 카메라 P90, 0, 1)
//
// 앵커 단은 차트(InferenceMonitor.tsx)·섀도 판정(feed_cycle/shadow_stop.go
// segmentChartValue)과 **같은 자**다. 세 곳이 같은 펄스에 다른 눈금을 매기면
// 운영자가 모달에서 본 값과 차트·판정이 어긋난다(2026-08-05 통일).
// 앵커 없는 카메라(미캘리브레이션 = tank_03/04)는 config 주석대로 종전 P90 폴백.
//
// ⚠ peak 은 **반응창 [+10s,+20s]** 의 최대여야 한다(호출부 vision_segments.go).
// 앵커가 그 창으로 캘리브레이션된 값이라, 세그먼트 전 구간의 최대를 넣으면
// 앵커를 넘어 1.00 에 포화한다(8-05 실측 20펄스 중 7개). 자와 창은 한 세트다.
func segmentPriorScore(baseline, peak, anchor, riseP90 float64) float64 {
	if anchor > 0 && baseline > 0 {
		return math.Max(0, math.Min(1, (peak-baseline)/anchor))
	}
	return calibratedRiseScore(vision_pipeline.PeakRiseFrac(baseline, peak), riseP90)
}

// riseAnchorByCamera — config feed_cycle.rise_anchor_by_camera (카메라별 반응 천장).
func (s *Server) riseAnchorByCamera() map[string]float64 {
	if s.cfg == nil {
		return nil
	}
	return s.cfg.FeedCycle.RiseAnchorByCamera
}

// riseP90Floor — 정규화 계수 하한. feed_cycle config 를 단일 출처로 쓴다
// (satiation_peak.go·InferenceMonitor.tsx 와 같은 값이어야 한다).
func (s *Server) riseP90Floor() float64 {
	if s.cfg == nil {
		return 0
	}
	return s.cfg.FeedCycle.SatiationRiseP90Floor
}
