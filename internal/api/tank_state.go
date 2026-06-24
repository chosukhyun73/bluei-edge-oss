package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

func (s *Server) handleTankRoute(w http.ResponseWriter, r *http.Request) {
	// /baseline/* 는 자체 메서드 검증 (POST score / GET status)
	if strings.Contains(r.URL.Path, "/baseline/") {
		s.handleTankBaselineRoute(w, r)
		return
	}
	// /autonomous-mode — GET + POST 자체 메서드 검증
	if strings.HasSuffix(r.URL.Path, "/autonomous-mode") {
		s.handleTankAutonomousMode(w, r)
		return
	}
	// /decision-policy — GET + POST (C-4)
	if strings.HasSuffix(r.URL.Path, "/decision-policy") {
		s.handleTankDecisionPolicy(w, r)
		return
	}
	// /decisions/* — propose, list-pending, resolve
	if strings.Contains(r.URL.Path, "/decisions/") || strings.HasSuffix(r.URL.Path, "/decisions") {
		s.handleTankDecisionRoute(w, r)
		return
	}
	// /stocking | /harvest | /lifecycle — D-1 lifecycle
	if strings.HasSuffix(r.URL.Path, "/stocking") || strings.HasSuffix(r.URL.Path, "/harvest") || strings.HasSuffix(r.URL.Path, "/lifecycle") {
		s.handleTankLifecycleRoute(w, r)
		return
	}
	// /treatment | /mortality | /transfer | /traceability | /documents — GDST first-mile CTE
	if strings.HasSuffix(r.URL.Path, "/treatment") || strings.HasSuffix(r.URL.Path, "/mortality") ||
		strings.HasSuffix(r.URL.Path, "/transfer") || strings.HasSuffix(r.URL.Path, "/traceability") ||
		strings.HasSuffix(r.URL.Path, "/documents") {
		s.handleTankTraceabilityRoute(w, r)
		return
	}
	// /sampling — D-2 sampling (POST + GET)
	if strings.HasSuffix(r.URL.Path, "/sampling") {
		s.handleTankSamplingRoute(w, r)
		return
	}
	// /weight-projection — D-3 FCR 기반 평균 체중 추정
	if strings.HasSuffix(r.URL.Path, "/weight-projection") {
		s.handleTankWeightProjection(w, r)
		return
	}
	// /weight-history — D-5 일일 추정 체중 시계열
	if strings.HasSuffix(r.URL.Path, "/weight-history") {
		s.handleTankWeightHistory(w, r)
		return
	}
	// /live-weight — B-8: tank 의 feeder controller UDP 잔량 (1Hz fresh)
	if strings.HasSuffix(r.URL.Path, "/live-weight") {
		s.handleTankLiveWeight(w, r)
		return
	}
	// /zero — 운영자 영점 버튼 (POST = set, DELETE = clear)
	if strings.HasSuffix(r.URL.Path, "/zero") {
		s.handleTankZero(w, r)
		return
	}
	// DELETE /v1/tanks/{tank_id} — 운영자 수조 삭제 (C-8)
	// path = "/v1/tanks/{id}" (no trailing segments). suffix check 로 분기.
	if r.Method == http.MethodDelete {
		rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/tanks/"), "/")
		// "/v1/tanks/{id}" 만 허용 (서브 path 는 위에서 이미 분기 처리됨)
		if rel != "" && !strings.Contains(rel, "/") {
			s.handleDeleteTank(w, r, rel)
			return
		}
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 나머지는 GET 전용
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/profile") {
		s.handleTankProfile(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/state-vector") {
		s.handleTankStateVector(w, r)
		return
	}
	s.handleTankState(w, r)
}

// handleDeleteTank — DELETE /v1/tanks/{tank_id}.
// 활성 feed_cycle 이 있으면 409 reject.
func (s *Server) handleDeleteTank(w http.ResponseWriter, r *http.Request, tankID string) {
	existing, err := s.store.GetTankProfile(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "TANK_NOT_FOUND", "수조를 찾을 수 없습니다", "")
		return
	}
	n, err := s.store.CountActiveFeedCyclesForTank(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "TANK_HAS_ACTIVE_CYCLE",
			"이 Cage/Tank 에 진행 중인 급이 사이클이 있어 삭제할 수 없습니다. 사이클을 중단한 후 다시 시도하세요.", "")
		return
	}
	if err := s.store.DeleteTankProfile(r.Context(), tankID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": tankID})
}

func (s *Server) handleTankState(w http.ResponseWriter, r *http.Request) {
	tankID := tankStateIDFromPath(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/state", "")
		return
	}

	readings, err := s.store.ListTankEnvironment(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if readings == nil {
		readings = []*storage.CurrentTankEnvironmentReading{}
	}

	devices, err := s.latestDeviceHealthForTank(r, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	cameras, err := s.store.ListCameraStatuses(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	alerts, err := s.store.ListOpenAlerts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	openAlerts := make([]*storage.OpenAlert, 0)
	for _, a := range alerts {
		if a.SubjectKind == "tank" && a.SubjectID == tankID {
			openAlerts = append(openAlerts, a)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id": tankID,
		"environment": map[string]any{
			"readings": readings,
			"count":    len(readings),
		},
		"devices": map[string]any{
			"items": devices,
			"count": len(devices),
		},
		"cameras": map[string]any{
			"items": cameras,
			"count": len(cameras),
		},
		"open_alerts": map[string]any{
			"items": openAlerts,
			"count": len(openAlerts),
		},
	})
}

func tankStateIDFromPath(path string) string {
	if !strings.HasPrefix(path, "/v1/tanks/") || !strings.HasSuffix(path, "/state") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/tanks/"), "/state")
	return strings.Trim(id, "/")
}

// latestDeviceHealthForTank — 한 Cage/Tank 의 가장 최근 device health.
// 2026-05-20: 이전 Limit=2000 은 device.health.updated 가 80k+ 누적된 환경에서 8초+ hang.
// events 테이블에 tank_id 컬럼이 없어 메모리 필터링 (N+1) 패턴. 정상 환경에서 한 tank
// 의 device 는 10개 이하라 Limit=200 이면 가장 최근 health 모두 cover.
func (s *Server) latestDeviceHealthForTank(r *http.Request, tankID string) ([]map[string]any, error) {
	healthEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventDeviceHealthUpdated,
		Limit:     50,
	})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	devices := make([]map[string]any, 0)
	for _, e := range healthEvents {
		var payload map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if stringValue(payload["tank_id"]) != tankID {
			continue
		}
		deviceID := stringValue(payload["device_id"])
		if deviceID == "" {
			continue
		}
		if _, ok := seen[deviceID]; ok {
			continue
		}
		seen[deviceID] = struct{}{}
		devices = append(devices, map[string]any{
			"sequence":     e.Sequence,
			"event_id":     e.EventID,
			"device_id":    deviceID,
			"device_type":  stringValue(payload["device_type"]),
			"status":       stringValue(payload["status"]),
			"quality":      stringValue(payload["quality"]),
			"last_seen_at": stringValue(payload["last_seen_at"]),
			"details":      payload["details"],
		})
	}
	return devices, nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
