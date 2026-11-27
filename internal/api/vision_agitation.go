package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

// parseSinceParam — ?since=RFC3339 를 EventFilter.Since 로. 없거나 파싱 실패면 nil
// (제한 없음) — 차트 조회라 잘못된 값에 500 을 내는 것보다 전체를 주는 편이 안전하다.
func parseSinceParam(r *http.Request) *time.Time {
	v := r.URL.Query().Get("since")
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return nil
	}
	return &t
}

// agitationEventPayload — vision.agitation.recorded 이벤트 payload 파싱용.
type agitationEventPayload struct {
	ClipID           string    `json:"clip_id"`
	TankID           string    `json:"tank_id"`
	CameraID         string    `json:"camera_id"`
	CapturedAt       string    `json:"captured_at"`
	NSeconds         int       `json:"n_seconds"`
	Baseline         float64   `json:"baseline"`
	Peak             float64   `json:"peak"`
	PeakAtS          int       `json:"peak_at_s"`
	Last5Mean        float64   `json:"last5_mean"`
	CalmReached      bool      `json:"calm_reached"`
	AUCAboveBaseline float64   `json:"auc_above_baseline"`
	Series           []float64 `json:"series"`
}

// handleVisionAgitation — GET /v1/vision/agitation?clip_id=...
//
// 해당 클립의 요동곡선(vision.agitation.recorded) 요약 + 제안 점수(먹이반응 prior)를 반환한다.
// 제안 점수는 "참고용 사전값"일 뿐 — 실제 라벨은 운영자가 화면(곡선)을 보고 확정한다.
// 곡선은 전체 활동 크기만 알려주며, 개체별 섭취/놀람 구분은 사람 보정(LRCN 정답 학습)이 필요.
func (s *Server) handleVisionAgitation(w http.ResponseWriter, r *http.Request) {
	clipID := r.URL.Query().Get("clip_id")
	if clipID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CLIP_ID", "clip_id query param required", "")
		return
	}

	evList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded,
		Limit:     500,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	var found *agitationEventPayload
	for _, e := range evList {
		var p agitationEventPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.ClipID == clipID {
			found = &p
			break
		}
	}
	if found == nil {
		writeJSON(w, http.StatusOK, map[string]any{"clip_id": clipID, "found": false})
		return
	}

	// 상대 피크 상승률 — vision_pipeline.PeakRiseFrac (사이클 보고와 공식 공유).
	peakRiseFrac := vision_pipeline.PeakRiseFrac(found.Baseline, found.Peak)
	// 제안 점수(먹이반응 prior) = 카메라 자기 기준 정규화 (rise_calibration.go).
	// 카메라별 동적범위 차이(tank_02 R크롭 +6~65% vs tank_01 +20~250%)로 저진폭
	// 카메라 점수가 눌리던 것을 자기 P90 스케일로 편다. P90 미확보 시 legacy clamp.
	riseP90 := s.riseP90ByCamera(r.Context())[found.CameraID]
	suggested := calibratedRiseScore(peakRiseFrac, riseP90)

	// 놀람·사람오염 의심 배지 — vision_pipeline.StartleSuspect (핸드오프 14:40 아티팩트
	// 근거, 점수엔 미반영 — 운영자 판단 보조 배지용).
	startleSuspect := vision_pipeline.StartleSuspect(found.CalmReached, found.NSeconds, found.PeakAtS)

	writeJSON(w, http.StatusOK, map[string]any{
		"clip_id":            found.ClipID,
		"found":              true,
		"n_seconds":          found.NSeconds,
		"baseline":           found.Baseline,
		"peak":               found.Peak,
		"peak_at_s":          found.PeakAtS,
		"last5_mean":         found.Last5Mean,
		"calm_reached":       found.CalmReached,
		"auc_above_baseline": found.AUCAboveBaseline,
		"peak_rise_frac":     math.Round(peakRiseFrac*1000) / 1000,
		"rise_p90":           math.Round(riseP90*1000) / 1000, // 0 = 캘리브레이션 미확보(legacy)
		"suggested_score":    math.Round(suggested*100) / 100,
		"startle_suspect":    startleSuspect,
		"series":             found.Series,
	})
}

// handleVisionAgitationRecent — GET /v1/vision/agitation/recent?limit=N&since=RFC3339&meta_only=1
//
// 최근 요동곡선(vision.agitation.recorded) 목록을 반환한다. 추론 모니터링 탭이
// 초 단위 series 를 (값−기저)/기저 정규화해 활동성 곡선으로 표시하는 용도.
//
// since / meta_only 는 화면이 보는 만큼만 받기 위한 것이다 (2026-08-14). 종전에는
// 기간 선택과 무관하게 항상 500건(실측 23일치 · 1.5MB · series 표본 249,499개)을
// 30초마다 받아 프론트가 관측 객체 5만 개를 새로 만들었고, 렌더러 메모리가
// 60~80MB/분 증가해 결국 WebKit 이 죽었다.
//   - since:     이 시각 이후 클립만 (1시간 화면이면 실제로 3건 안팎)
//   - meta_only: series 를 뺀다. P90 보정처럼 대표값(peak/baseline)만 필요한 조회용
func (s *Server) handleVisionAgitationRecent(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	since := parseSinceParam(r)
	metaOnly := r.URL.Query().Get("meta_only") == "1"

	evList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded,
		Limit:     limit,
		Since:     since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	items := make([]map[string]any, 0, len(evList))
	for _, e := range evList {
		var p agitationEventPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		// EventFilter.Since 는 이벤트 적재 시각으로 거른다. 차트는 촬영 시각(captured_at)에
		// 점을 찍으므로 여기서 한 번 더 거른다 — 백필로 늦게 적재된 오래된 클립이
		// 창 안으로 딸려 들어오지 않게. 파싱 실패는 남긴다(fail-open).
		if since != nil && p.CapturedAt != "" {
			if ct, err := time.Parse(time.RFC3339Nano, p.CapturedAt); err == nil && ct.Before(*since) {
				continue
			}
		}
		item := map[string]any{
			"clip_id":     p.ClipID,
			"camera_id":   p.CameraID,
			"captured_at": p.CapturedAt,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"n_seconds":   p.NSeconds,
			"baseline":    p.Baseline,
			"peak":        p.Peak,
		}
		if !metaOnly {
			item["series"] = p.Series
		}
		items = append(items, item)
	}
	// rise_p90_floor — 프론트가 자기 계산한 P90 에 적용할 하한 (2026-08-02).
	// 종전에는 프론트가 상수로 들고 있어 백엔드와 어긋날 수 있었다. 여기서 내려
	// 단일 출처(config feed_cycle.satiation_rise_p90_floor)로 통일한다.
	//
	// rise_anchor_by_camera — 카메라별 반응 천장 (2026-08-05). 있으면 프론트는
	// P90 계산을 건너뛰고 이 값으로 나눈다. 같은 이유로 여기서 내린다 — 화면 눈금과
	// 섀도 판정(feed_cycle/shadow_stop.go)이 같은 자를 써야 운영자가 차트에서 본
	// 높이가 곧 판정 입력이 된다.
	writeJSON(w, http.StatusOK, map[string]any{
		"count":                 len(items),
		"items":                 items,
		"rise_p90_floor":        s.riseP90Floor(),
		"rise_anchor_by_camera": s.cfg.FeedCycle.RiseAnchorByCamera,
	})
}

// handleVisionAgitationLive — GET /v1/vision/agitation/live
//
// 진행 중 사이클의 실시간 요동 series (in-memory LiveAgitation 세션). 추론
// 모니터링 탭이 급이 중 폴링해 녹색 곡선을 실시간 표기한다 (사이클 종료 후에야
// 나오던 vision.agitation.recorded 를 기다리지 않고). series 는 raw — 프론트가
// 클립과 동일한 P90 정규화를 적용한다. provider 미설정 시 빈 목록.
func (s *Server) handleVisionAgitationLive(w http.ResponseWriter, r *http.Request) {
	var items []vision_pipeline.LiveAgitationSnapshot
	if s.liveAgi != nil {
		items = s.liveAgi()
	}
	if items == nil {
		items = []vision_pipeline.LiveAgitationSnapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items})
}

// agitationBaselinePayload — vision.agitation.baseline.recorded payload 파싱용.
type agitationBaselinePayload struct {
	ClipID     string  `json:"clip_id"`
	TankID     string  `json:"tank_id"`
	CameraID   string  `json:"camera_id"`
	CapturedAt string  `json:"captured_at"`
	NSeconds   int     `json:"n_seconds"`
	Mean       float64 `json:"mean"`
	Peak       float64 `json:"peak"`
	Night      bool    `json:"night"` // 야간(IR) 측정 — 주간과 스케일 상이
}

// handleVisionAgitationBaseline — GET /v1/vision/agitation/baseline?limit=N
//
// 평소 활동성(baseline 클립 요동 대표값) 목록. 추론 모니터링 탭이 카메라별
// 롤링 기저(중앙값) 대비 비율로 정규화해 선형 차트로 표시한다.
//
// ⚠ recent 와 달리 기간 축소(since)를 넣지 않았다 — 여기서 받은 표본의 **중앙값**이
// "1.0 = 평소"의 기준이라, 창을 좁히면 같은 데이터가 다른 높이로 그려진다.
func (s *Server) handleVisionAgitationBaseline(w http.ResponseWriter, r *http.Request) {
	limit := 3000
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20000 {
			limit = n
		}
	}

	evList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: vision_pipeline.EventAgitationBaselineRecorded,
		Limit:     limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	items := make([]map[string]any, 0, len(evList))
	for _, e := range evList {
		var p agitationBaselinePayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		items = append(items, map[string]any{
			"clip_id":     p.ClipID,
			"tank_id":     p.TankID,
			"camera_id":   p.CameraID,
			"captured_at": p.CapturedAt,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"n_seconds":   p.NSeconds,
			"mean":        p.Mean,
			"peak":        p.Peak,
			"night":       p.Night,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items})
}

// handleVisionAgitationBackfill — POST /v1/vision/agitation/backfill
//
// 파이프라인 실가동 이전에 저장된 급이(feeding) 클립에 요동 곡선을 소급 생성한다.
// mp4 파일이 남아 있는 클립만 대상. 이미 곡선이 있으면 skip(멱등). 라벨링 보조용.
func (s *Server) handleVisionAgitationBackfill(w http.ResponseWriter, r *http.Request) {
	// 1. 이미 요동곡선이 있는 clip_id 집합.
	existing := map[string]struct{}{}
	agEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded, Limit: 2000,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, e := range agEvents {
		var p agitationEventPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil && p.ClipID != "" {
			existing[p.ClipID] = struct{}{}
		}
	}

	// 2. feeding 클립 조회 (phase 필터 — baseline 에 파묻히지 않음).
	clipEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventMediaClipStored, EvidencePhase: "feeding", Limit: 2000,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 대상 worklist 구성 (동기, 빠름).
	var work []vision_pipeline.AgitationClipMeta
	var skipped int
	for _, e := range clipEvents {
		var p events.MediaClipStoredPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if _, ok := existing[p.ClipID]; ok {
			skipped++ // 이미 곡선 있음 / worklist 중복
			continue
		}
		if p.URI == "" {
			skipped++
			continue
		}
		if _, statErr := os.Stat(p.URI); statErr != nil {
			skipped++ // 파일 없음(retention 정리)
			continue
		}
		existing[p.ClipID] = struct{}{} // worklist 중복 방지
		cycleID, _ := p.Evidence["cycle_id"].(string)
		work = append(work, vision_pipeline.AgitationClipMeta{
			ClipID: p.ClipID, CycleID: cycleID, TankID: p.TankID,
			CameraID: p.CameraID, MP4Path: p.URI, CapturedAt: p.StartedAt,
		})
	}

	// 스크립트 ~0.7s/클립 × N 이 30s WriteTimeout 을 넘길 수 있어 백그라운드 처리.
	go func(items []vision_pipeline.AgitationClipMeta) {
		var ok, fail int
		for _, meta := range items {
			if err := vision_pipeline.BackfillAgitation(context.Background(), s.app, meta); err != nil {
				fail++
				slog.Warn("agitation backfill clip failed", "clip_id", meta.ClipID, "error", err)
				continue
			}
			ok++
		}
		slog.Info("agitation backfill complete", "backfilled", ok, "failed", fail, "candidates", len(items))
	}(work)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "started",
		"candidates": len(work),
		"skipped":    skipped,
	})
}
