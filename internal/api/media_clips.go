package api

import (
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
)

type mediaClipRequest struct {
	ClipID     string         `json:"clip_id"`
	CameraID   string         `json:"camera_id"`
	TankID     string         `json:"tank_id"`
	Reason     string         `json:"reason"`
	StartedAt  string         `json:"started_at"`
	EndedAt    string         `json:"ended_at"`
	URI        string         `json:"uri"`
	MimeType   string         `json:"mime_type"`
	SizeBytes  int64          `json:"size_bytes"`
	FrameCount int            `json:"frame_count"`
	Evidence   map[string]any `json:"evidence"`
}

func (s *Server) handleMediaClipsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRecentMediaClips(w, r)
	case http.MethodPost:
		s.handlePostMediaClip(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePostMediaClip(w http.ResponseWriter, r *http.Request) {
	var req mediaClipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.ClipID == "" {
		req.ClipID = common.NewID("media_clip")
	}
	now := common.FormatTime(common.NowUTC())
	if req.StartedAt == "" {
		req.StartedAt = now
	}
	if req.EndedAt == "" {
		req.EndedAt = now
	}
	payload := events.MediaClipStoredPayload{
		ClipID:     req.ClipID,
		CameraID:   req.CameraID,
		TankID:     req.TankID,
		Reason:     req.Reason,
		StartedAt:  req.StartedAt,
		EndedAt:    req.EndedAt,
		URI:        req.URI,
		MimeType:   req.MimeType,
		SizeBytes:  req.SizeBytes,
		FrameCount: req.FrameCount,
		Evidence:   req.Evidence,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_MEDIA_CLIP", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "media", payload.CameraID, events.EventMediaClipStored, payload.ClipID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sequence": seq, "clip": payload})
}
