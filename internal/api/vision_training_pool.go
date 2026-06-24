package api

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// handleVisionTrainingPool — R6.3.
// GET /v1/vision/training-pool?phase=feeding|baseline&limit=N
//
// LRCN 지도학습용 영상 풀에서 랜덤 샘플링.
//   - phase=feeding  → cycle 활성 중 캡처된 mp4 (사료 반응 평가용)
//   - phase=baseline → cycle 외 시간에 캡처된 mp4 (안정성 평가용)
//
// source: events.media.clip.stored 의 evidence.phase 필드.
// (cycle hook + 상시 캡처 모두 동일 event 로 적재, R6.2 wire.)
//
// limit 범위 [1, 50]. 기본 10.
// tank_id / camera_id 필터링 옵션.
func (s *Server) handleVisionTrainingPool(w http.ResponseWriter, r *http.Request) {
	phase := strings.ToLower(r.URL.Query().Get("phase"))
	if phase != "feeding" && phase != "baseline" {
		writeError(w, http.StatusBadRequest, "INVALID_PHASE",
			"phase must be 'feeding' or 'baseline'", "")
		return
	}
	limit := intParam(r, "limit", 10, 50)
	tankID := r.URL.Query().Get("tank_id")
	cameraID := r.URL.Query().Get("camera_id")

	// R8.2 — excluded clip_id set 구축. media.clip.excluded events 의 clip_id 는 풀에서 제거.
	excluded := make(map[string]struct{})
	excludedEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventMediaClipExcluded,
		Limit:     500,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, e := range excludedEvents {
		var p events.MediaClipExcludedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		excluded[p.ClipID] = struct{}{}
	}

	// 충분히 큰 후보 풀에서 랜덤 sampling — events 200건 까지 fetch 후 filter+shuffle.
	candidatePool := 200
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventMediaClipStored,
		Limit:     candidatePool,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	matches := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.MediaClipStoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		// R8.2: excluded 영상 제거
		if _, isExcluded := excluded[payload.ClipID]; isExcluded {
			continue
		}
		// evidence.phase 필터링
		ev := payload.Evidence
		evPhase, _ := ev["phase"].(string)
		if !strings.EqualFold(evPhase, phase) {
			continue
		}
		if tankID != "" && payload.TankID != tankID {
			continue
		}
		if cameraID != "" && payload.CameraID != cameraID {
			continue
		}
		matches = append(matches, map[string]any{
			"sequence":    e.Sequence,
			"event_id":    e.EventID,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"payload":     payload,
		})
	}

	// 랜덤 셔플 + limit. seed 는 매 요청마다 다름 (운영자가 같은 영상 반복 안 받게).
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(matches), func(i, j int) { matches[i], matches[j] = matches[j], matches[i] })
	if len(matches) > limit {
		matches = matches[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"phase":            phase,
		"count":            len(matches),
		"candidate_pool":   candidatePool,
		"items":            matches,
		"tank_id_filter":   tankID,
		"camera_id_filter": cameraID,
	})
}
