package api

// 펄스별 r3 채점 목록 — 추론 모니터링 탭 표시용 (2026-07-20 운영자:
// LRCN 반응 평가의 정본은 펄스별 세그먼트 채점이고 통클립 1점은 보조).
//
// vision.segment.model_scores.recorded (리포터가 보고 직후 적재, 사이클당 1건에
// 펄스별 scores 배열) 를 feed.pulse.started 의 펄스 시작 시각과 조인해 펄스당
// 1행으로 평탄화한다.

import (
	"encoding/json"
	"net/http"
	"strconv"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type segmentModelScoresPayload struct {
	CycleID      string `json:"cycle_id"`
	TankID       string `json:"tank_id"`
	CameraID     string `json:"camera_id"`
	ModelVersion string `json:"model_version"`
	Scores       []struct {
		PulseNo      int      `json:"pulse_no"`
		FeedingScore *float64 `json:"feeding_score"` // null = 채점 실패 세그먼트
	} `json:"scores"`
}

type pulseStartedPayload struct {
	CycleID   string `json:"cycle_id"`
	PulseNo   int    `json:"pulse_no"`
	StartedAt string `json:"started_at"`
}

// handleVisionModelScoresRecent — GET /v1/vision/model-scores/recent?limit=N
// 최근 사이클들의 펄스별 모델 점수 (펄스 시작 시각 포함, 펄스당 1행).
func (s *Server) handleVisionModelScoresRecent(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	msEvs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionSegmentScoresRecorded,
		Limit:     limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 펄스 시작 시각 조인용 — 최근 2000건 (~3주 분량, model_scores 커버리지 초과).
	pulseAt := map[string]string{} // "cycle_id|pulse_no" → started_at
	if pEvs, perr := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: "feed.pulse.started",
		Limit:     2000,
	}); perr == nil {
		for _, e := range pEvs {
			var p pulseStartedPayload
			if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil && p.CycleID != "" {
				pulseAt[p.CycleID+"|"+strconv.Itoa(p.PulseNo)] = p.StartedAt
			}
		}
	}

	items := make([]map[string]any, 0, len(msEvs)*4)
	for _, e := range msEvs {
		var p segmentModelScoresPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		for _, sc := range p.Scores {
			if sc.FeedingScore == nil {
				continue
			}
			items = append(items, map[string]any{
				"cycle_id":      p.CycleID,
				"tank_id":       p.TankID,
				"camera_id":     p.CameraID,
				"model_version": p.ModelVersion,
				"pulse_no":      sc.PulseNo,
				"feeding_score": *sc.FeedingScore,
				"pulse_at":      pulseAt[p.CycleID+"|"+strconv.Itoa(sc.PulseNo)], // "" = 조인 실패
				"recorded_at":   common.FormatTime(e.RecordedAt),
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items})
}
