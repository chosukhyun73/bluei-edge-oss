package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
)

func (s *Server) handleOpenAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.store.ListOpenAlerts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]any, 0, len(alerts))
	for _, a := range alerts {
		var payload map[string]any
		_ = json.Unmarshal([]byte(a.PayloadJSON), &payload)
		message, _ := payload["message"].(string)
		evidence, _ := payload["evidence"].(map[string]any)
		out = append(out, map[string]any{
			"alert_id":   a.AlertID,
			"alert_type": a.AlertType,
			"severity":   a.Severity,
			"status":     a.Status,
			"subject":    map[string]any{"kind": a.SubjectKind, "id": a.SubjectID},
			"rule_id":    a.RuleID,
			"message":    message,
			"evidence":   evidence,
			"raised_at":  common.FormatTime(a.RaisedAt),
			"updated_at": common.FormatTime(a.UpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": out, "count": len(out)})
}

// handleAlertItemRoute — /v1/alerts/{alert_id}/close 등 single-alert 액션 라우팅.
func (s *Server) handleAlertItemRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/alerts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[1] == "close" && r.Method == http.MethodPost {
		s.handleCloseAlert(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "alert route not found", "")
}

// handleCloseAlert — 운영자 명시적 close. open_alerts 에서 alert_id 매칭 row 제거 +
// alert.updated event (status=resolved, resolution_reason=operator_dismiss) 적재.
// alert 없거나 이미 close 면 200 + dismissed:false.
func (s *Server) handleCloseAlert(w http.ResponseWriter, r *http.Request, alertID string) {
	if alertID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "alert_id required", "")
		return
	}
	ctx := r.Context()
	existing, err := s.store.GetOpenAlertByID(ctx, alertID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"alert_id":  alertID,
			"dismissed": false,
			"reason":    "not_open",
		})
		return
	}
	if _, err := s.store.ClearAlertByID(ctx, alertID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	now := common.NowUTC()
	payload := map[string]any{
		"alert_id":   existing.AlertID,
		"alert_type": existing.AlertType,
		"severity":   existing.Severity,
		"status":     "resolved",
		"subject":    map[string]any{"kind": existing.SubjectKind, "id": existing.SubjectID},
		"message":    fmt.Sprintf("운영자가 알림 close — %s/%s", existing.SubjectKind, existing.SubjectID),
		"evidence": map[string]any{
			"resolution_reason": "operator_dismiss",
			"closed_at":         common.FormatTime(now),
		},
		"raised_at":  common.FormatTime(existing.RaisedAt),
		"updated_at": common.FormatTime(now),
	}
	if s.app != nil {
		_, _ = s.app.AppendEvent(ctx, "operator", "", existing.SubjectID, "alert.updated", existing.AlertID, payload)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alert_id":  alertID,
		"dismissed": true,
	})
}
