package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// tankAutonomousModeIDFromPath — /v1/tanks/{id}/autonomous-mode 에서 tank_id 추출.
func tankAutonomousModeIDFromPath(path string) string {
	if !strings.HasPrefix(path, "/v1/tanks/") || !strings.HasSuffix(path, "/autonomous-mode") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/tanks/"), "/autonomous-mode")
	return strings.Trim(id, "/")
}

type tankAutonomousModeRequest struct {
	Mode       string `json:"mode"`
	Reason     string `json:"reason"`
	OperatorID string `json:"operator_id"`
}

func (s *Server) handleTankAutonomousMode(w http.ResponseWriter, r *http.Request) {
	tankID := tankAutonomousModeIDFromPath(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{tank_id}/autonomous-mode", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getTankAutonomousMode(w, r, tankID)
	case http.MethodPost:
		s.postTankAutonomousMode(w, r, tankID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getTankAutonomousMode — 현재 모드 반환. 행 없으면 기본값 off 로 합성.
func (s *Server) getTankAutonomousMode(w http.ResponseWriter, r *http.Request, tankID string) {
	row, err := s.store.GetTankAutonomousMode(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if row == nil {
		// 기본값 — 아직 모드 미설정 (off).
		writeJSON(w, http.StatusOK, map[string]any{
			"tank_id":    tankID,
			"mode":       "off",
			"changed_at": "",
			"changed_by": "",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":    row.TankID,
		"mode":       row.Mode,
		"reason":     row.Reason,
		"changed_at": fmtAPITime(row.ChangedAt),
		"changed_by": row.ChangedBy,
	})
}

// postTankAutonomousMode — 모드 변경 + audit 이벤트 적재 + projection upsert.
func (s *Server) postTankAutonomousMode(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankAutonomousModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	// 모드 검증
	if !events.ValidAutonomousMode(req.Mode) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_MODE",
			"mode must be one of: off, observation, partial, full", "")
		return
	}

	// 운영자 기본값
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}

	// off 외 전환은 reason 필수
	if req.Mode != "off" && req.Reason == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_REASON",
			"reason is required for non-off mode transitions", "")
		return
	}

	// 현재 상태 조회
	prev, err := s.store.GetTankAutonomousMode(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	prevMode := ""
	if prev != nil {
		prevMode = prev.Mode
	}

	// 동일 모드 — 이벤트/쓰기 생략
	if prev != nil && prev.Mode == req.Mode {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"no_change": true,
			"current": map[string]any{
				"tank_id":    prev.TankID,
				"mode":       prev.Mode,
				"reason":     prev.Reason,
				"changed_at": fmtAPITime(prev.ChangedAt),
				"changed_by": prev.ChangedBy,
			},
		})
		return
	}

	now := common.NowUTC()
	payload := events.TankAutonomousModeChangedPayload{
		TankID:       tankID,
		PreviousMode: prevMode,
		NewMode:      req.Mode,
		OperatorID:   req.OperatorID,
		Reason:       req.Reason,
		ChangedAt:    now.Format(time.RFC3339Nano),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "autonomous_mode", tankID,
		events.EventTankAutonomousModeChanged, tankID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	row := &storage.TankAutonomousMode{
		TankID:    tankID,
		Mode:      req.Mode,
		Reason:    req.Reason,
		ChangedAt: now,
		ChangedBy: req.OperatorID,
	}
	if err := s.store.UpsertTankAutonomousMode(r.Context(), row); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"sequence": seq,
		"applied": map[string]any{
			"tank_id":    row.TankID,
			"mode":       row.Mode,
			"reason":     row.Reason,
			"changed_at": fmtAPITime(row.ChangedAt),
			"changed_by": row.ChangedBy,
		},
	})
}

// fmtAPITime — time.Time 을 RFC3339Nano 문자열로 변환. zero value 면 빈 문자열.
func fmtAPITime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
