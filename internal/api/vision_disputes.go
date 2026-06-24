package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type visionObservationDisputeRequest struct {
	DisputeID     string   `json:"dispute_id"`
	CameraID      string   `json:"camera_id"`
	TankID        string   `json:"tank_id"`
	OperatorID    string   `json:"operator_id"`
	Verdict       string   `json:"verdict"`
	Reason        string   `json:"reason"`
	OperatorScore *float64 `json:"operator_score,omitempty"` // G-4: 단일 슬라이더 (legacy)
	PreScore      *float64 `json:"pre_score,omitempty"`      // R5: 급이 전 안정성 0~1
	DuringScore   *float64 `json:"during_score,omitempty"`   // R5: 급이 도중 반응 0~1
	ClipRef       string   `json:"clip_ref,omitempty"`       // R5: observation clip_ref 정합
	AlgorithmID   string   `json:"algorithm_id,omitempty"`   // R11: 어느 LRCN 모델용 라벨인지
	DisputedAt    string   `json:"disputed_at"`
}

func (s *Server) handleVisionObservationRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/vision/observations/"), "/")
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/vision/observations/{observation_id}/{disputes|replay|clip.mp4}", "")
		return
	}
	switch parts[1] {
	case "disputes":
		switch r.Method {
		case http.MethodGet:
			s.handleListVisionObservationDisputes(w, r, parts[0])
		case http.MethodPost:
			s.handlePostVisionObservationDispute(w, r, parts[0])
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "replay":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleVisionObservationReplay(w, r, parts[0])
	case "clip.mp4":
		// HEAD 도 허용 — HTML5 <video> 가 시작 시 Range/size probe.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleVisionObservationClip(w, r, parts[0])
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected disputes, replay or clip.mp4 route", "")
	}
}

func (s *Server) handlePostVisionObservationDispute(w http.ResponseWriter, r *http.Request, observationID string) {
	var req visionObservationDisputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.DisputeID == "" {
		req.DisputeID = common.NewID("vis_dispute")
	}
	if req.DisputedAt == "" {
		req.DisputedAt = common.FormatTime(common.NowUTC())
	}
	payload := events.VisionObservationDisputedPayload{
		DisputeID:     req.DisputeID,
		ObservationID: observationID,
		CameraID:      req.CameraID,
		TankID:        req.TankID,
		OperatorID:    req.OperatorID,
		Verdict:       req.Verdict,
		Reason:        req.Reason,
		OperatorScore: req.OperatorScore,
		PreScore:      req.PreScore,
		DuringScore:   req.DuringScore,
		ClipRef:       req.ClipRef,
		AlgorithmID:   req.AlgorithmID,
		DisputedAt:    req.DisputedAt,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_VISION_OBSERVATION_DISPUTE", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "vision", payload.CameraID, events.EventVisionObservationDisputed, payload.DisputeID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sequence": seq, "dispute": payload})
}

func (s *Server) handleListVisionObservationDisputes(w http.ResponseWriter, r *http.Request, observationID string) {
	limit := intParam(r, "limit", 20, 100)
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventVisionObservationDisputed, Limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	items := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.VisionObservationDisputedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload.ObservationID != observationID {
			continue
		}
		items = append(items, map[string]any{"sequence": e.Sequence, "event_id": e.EventID, "recorded_at": common.FormatTime(e.RecordedAt), "payload": payload})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "observation_id": observationID})
}

func (s *Server) handleVisionObservationReplay(w http.ResponseWriter, r *http.Request, observationID string) {
	observation, err := s.findVisionObservation(r, observationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if observation == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "vision observation not found", "")
		return
	}
	disputes := s.collectVisionDisputes(r, observationID, 100)
	clips := s.collectRelatedMediaClips(r, observation.Payload)
	writeJSON(w, http.StatusOK, map[string]any{
		"observation_id": observationID,
		"observation":    observation,
		"disputes":       disputes,
		"clips":          clips,
	})
}

type replayObservation struct {
	Sequence   int64                           `json:"sequence"`
	EventID    string                          `json:"event_id"`
	RecordedAt string                          `json:"recorded_at"`
	Payload    events.VisionObservationPayload `json:"payload"`
}

func (s *Server) findVisionObservation(r *http.Request, observationID string) (*replayObservation, error) {
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventVisionObservationRecorded, Limit: 500})
	if err != nil {
		return nil, err
	}
	for _, e := range eventsList {
		var payload events.VisionObservationPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload.ObservationID == observationID {
			return &replayObservation{Sequence: e.Sequence, EventID: e.EventID, RecordedAt: common.FormatTime(e.RecordedAt), Payload: payload}, nil
		}
	}
	return nil, nil
}

func (s *Server) collectVisionDisputes(r *http.Request, observationID string, limit int) []map[string]any {
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventVisionObservationDisputed, Limit: limit})
	if err != nil {
		return nil
	}
	items := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.VisionObservationDisputedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil || payload.ObservationID != observationID {
			continue
		}
		items = append(items, map[string]any{"sequence": e.Sequence, "event_id": e.EventID, "recorded_at": common.FormatTime(e.RecordedAt), "payload": payload})
	}
	return items
}

func (s *Server) collectRelatedMediaClips(r *http.Request, observation events.VisionObservationPayload) []map[string]any {
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventMediaClipStored, Limit: 200})
	if err != nil {
		return nil
	}
	items := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.MediaClipStoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if observation.ClipRef != "" && payload.ClipID == observation.ClipRef {
			items = append(items, map[string]any{"sequence": e.Sequence, "event_id": e.EventID, "recorded_at": common.FormatTime(e.RecordedAt), "payload": payload})
			continue
		}
		if observation.CameraID != "" && payload.CameraID == observation.CameraID && observation.TankID != "" && payload.TankID == observation.TankID {
			items = append(items, map[string]any{"sequence": e.Sequence, "event_id": e.EventID, "recorded_at": common.FormatTime(e.RecordedAt), "payload": payload})
		}
	}
	return items
}
