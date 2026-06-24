package api

import (
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type visionObservationRequest struct {
	ObservationID string             `json:"observation_id"`
	CameraID      string             `json:"camera_id"`
	TankID        string             `json:"tank_id"`
	Mode          string             `json:"mode"`
	Phase         string             `json:"phase"`
	ObservedAt    string             `json:"observed_at"`
	FrameTS       string             `json:"frame_ts"`
	FrameRef      string             `json:"frame_ref"`
	ClipRef       string             `json:"clip_ref"`
	ModelVersion  string             `json:"model_version"`
	Confidence    *float64           `json:"confidence"`
	Scores        map[string]float64 `json:"scores"`
	Candidates    []string           `json:"candidates"`
	Evidence      map[string]any     `json:"evidence"`
	Quality       string             `json:"quality"`
}

func (s *Server) handlePostVisionObservation(w http.ResponseWriter, r *http.Request) {
	var req visionObservationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	payload := visionObservationPayloadFromRequest(req)
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_VISION_OBSERVATION", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "vision", payload.CameraID, events.EventVisionObservationRecorded, payload.ObservationID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sequence": seq, "observation": payload})
}

func (s *Server) handleRecentVisionObservations(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 20, 100)
	tankID := r.URL.Query().Get("tank_id")
	items, err := s.listVisionObservations(r, tankID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "tank_id": tankID})
}

func visionObservationPayloadFromRequest(req visionObservationRequest) events.VisionObservationPayload {
	if req.ObservationID == "" {
		req.ObservationID = common.NewID("vis_obs")
	}
	if req.Mode == "" {
		req.Mode = "lightweight"
	}
	if req.Phase == "" {
		req.Phase = "normal"
	}
	if req.ObservedAt == "" {
		req.ObservedAt = common.FormatTime(common.NowUTC())
	}
	if req.Quality == "" {
		req.Quality = events.QualityOK
	}
	return events.VisionObservationPayload{
		ObservationID: req.ObservationID,
		CameraID:      req.CameraID,
		TankID:        req.TankID,
		Mode:          req.Mode,
		Phase:         req.Phase,
		ObservedAt:    req.ObservedAt,
		FrameTS:       req.FrameTS,
		FrameRef:      req.FrameRef,
		ClipRef:       req.ClipRef,
		ModelVersion:  req.ModelVersion,
		Confidence:    req.Confidence,
		Scores:        req.Scores,
		Candidates:    req.Candidates,
		Evidence:      req.Evidence,
		Quality:       req.Quality,
	}
}

func (s *Server) listVisionObservations(r *http.Request, tankID string, limit int) ([]map[string]any, error) {
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventVisionObservationRecorded, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.VisionObservationPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if tankID != "" && payload.TankID != tankID {
			continue
		}
		out = append(out, map[string]any{
			"sequence":    e.Sequence,
			"event_id":    e.EventID,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"payload":     payload,
		})
	}
	return out, nil
}

func (s *Server) handleVisionObservationsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRecentVisionObservations(w, r)
	case http.MethodPost:
		s.handlePostVisionObservation(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
