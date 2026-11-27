package api

import (
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 세그먼트 라벨 저장/조회 (docs/50 3단계). 통클립 1점 dispute 대신 펄스 세그먼트별
// 사료반응 라벨을 사이클+카메라 단위 1이벤트로 저장.
//
//	POST /v1/vision/segment-labels          — 저장
//	GET  /v1/vision/segment-labels?cycle_id= — 그 사이클의 기존 라벨(큐 중복 회피용)

type segmentLabelRequest struct {
	CycleID     string                `json:"cycle_id"`
	ClipID      string                `json:"clip_id"`
	CameraID    string                `json:"camera_id"`
	TankID      string                `json:"tank_id"`
	OperatorID  string                `json:"operator_id"`
	AlgorithmID string                `json:"algorithm_id"`
	Segments    []events.SegmentLabel `json:"segments"`
	// 운영자가 배출 ✓/✗ 를 실제로 고쳤는가. 미전송(nil)이면 이 라벨은 정답지로 쓸 수 없다.
	DischargeModified *bool `json:"discharge_modified,omitempty"`
}

func (s *Server) handleVisionSegmentLabels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostVisionSegmentLabels(w, r)
	case http.MethodGet:
		s.handleGetVisionSegmentLabels(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePostVisionSegmentLabels(w http.ResponseWriter, r *http.Request) {
	var req segmentLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	payload := events.VisionSegmentLabelPayload{
		LabelID:     common.NewID("vis_seglabel"),
		CycleID:     req.CycleID,
		ClipID:      req.ClipID,
		CameraID:    req.CameraID,
		TankID:      req.TankID,
		OperatorID:  req.OperatorID,
		AlgorithmID: req.AlgorithmID,
		Segments:    req.Segments,
		LabeledAt:   common.FormatTime(common.NowUTC()),

		DischargeModified: req.DischargeModified,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SEGMENT_LABEL", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "vision", payload.CameraID,
		events.EventVisionSegmentLabelRecorded, payload.LabelID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sequence": seq, "label": payload})
}

func (s *Server) handleGetVisionSegmentLabels(w http.ResponseWriter, r *http.Request) {
	cycleID := r.URL.Query().Get("cycle_id")
	if cycleID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CYCLE_ID", "cycle_id query param required", "")
		return
	}
	evList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:      events.EventVisionSegmentLabelRecorded,
		PayloadCycleID: cycleID,
		Limit:          50,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	items := make([]events.VisionSegmentLabelPayload, 0, len(evList))
	for _, e := range evList {
		var p events.VisionSegmentLabelPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil {
			items = append(items, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cycle_id": cycleID,
		"labeled":  len(items) > 0,
		"items":    items,
	})
}
