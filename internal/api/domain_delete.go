package api

import (
	"net/http"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// C-8 CRUD 일관성 — 도메인 DELETE 핸들러 묶음.
//
// 정책: FK 의존성이 있는 자식 row 가 있으면 409 reject. cascade 금지.
// 모든 응답은 200 + {ok:true, deleted:"<id>"} 또는 표준 error envelope.
// path-param 추출은 strings.TrimPrefix + Trim("/"). 단일 id 만 허용.
// ─────────────────────────────────────────────────────────────────────────────

// extractID returns the path segment after the given prefix, or "" if invalid
// (empty / contains nested path).
func extractID(path, prefix string) string {
	rel := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rel == "" || strings.Contains(rel, "/") {
		return ""
	}
	return rel
}

// ── /v1/farms/{id} ─────────────────────────────────────────────────────────

func (s *Server) handleFarmItem(w http.ResponseWriter, r *http.Request) {
	farmID := extractID(r.URL.Path, "/v1/farms/")
	if farmID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/farms/{farm_id}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteFarm(w, r, farmID)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteFarm(w http.ResponseWriter, r *http.Request, farmID string) {
	existing, err := s.store.GetFarm(r.Context(), farmID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "FARM_NOT_FOUND", "농장을 찾을 수 없습니다", "")
		return
	}
	n, err := s.store.CountSitesForFarm(r.Context(), farmID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "FARM_HAS_SITES",
			"이 농장에 속한 사이트가 있어 삭제할 수 없습니다. 먼저 사이트를 삭제하세요.", "")
		return
	}
	if err := s.store.DeleteFarm(r.Context(), farmID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": farmID})
}

// ── /v1/sites/{id} ─────────────────────────────────────────────────────────

func (s *Server) handleSiteItem(w http.ResponseWriter, r *http.Request) {
	siteID := extractID(r.URL.Path, "/v1/sites/")
	if siteID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/sites/{site_id}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteSite(w, r, siteID)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request, siteID string) {
	exists, err := s.store.SiteExists(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "SITE_NOT_FOUND", "사이트를 찾을 수 없습니다", "")
		return
	}
	// FK 의존성: WTG, Tank
	if n, err := s.store.CountWTGsForSite(r.Context(), siteID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "SITE_HAS_WTGS",
			"이 사이트에 속한 수처리 그룹(WTG)이 있어 삭제할 수 없습니다. 먼저 WTG 를 삭제하세요.", "")
		return
	}
	if n, err := s.store.CountTanksForSite(r.Context(), siteID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "SITE_HAS_TANKS",
			"이 사이트에 속한 Cage/Tank 가 있어 삭제할 수 없습니다. 먼저 수조를 삭제하세요.", "")
		return
	}
	if err := s.store.DeleteSite(r.Context(), siteID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": siteID})
}

// ── /v1/water-treatment-groups/{id} ─────────────────────────────────────────

func (s *Server) handleWTGItem(w http.ResponseWriter, r *http.Request) {
	wtgID := extractID(r.URL.Path, "/v1/water-treatment-groups/")
	if wtgID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/water-treatment-groups/{wtg_id}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteWTG(w, r, wtgID)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteWTG(w http.ResponseWriter, r *http.Request, wtgID string) {
	exists, err := s.store.WTGExists(r.Context(), wtgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "WTG_NOT_FOUND", "수처리 그룹을 찾을 수 없습니다", "")
		return
	}
	if n, err := s.store.CountTanksForWTG(r.Context(), wtgID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "WTG_HAS_TANKS",
			"이 WTG 에 속한 Cage/Tank 가 있어 삭제할 수 없습니다. 먼저 수조를 삭제하세요.", "")
		return
	}
	if err := s.store.DeleteWTG(r.Context(), wtgID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": wtgID})
}

// ── /v1/sensors/{id} ────────────────────────────────────────────────────────

func (s *Server) handleSensorItem(w http.ResponseWriter, r *http.Request) {
	sensorID := extractID(r.URL.Path, "/v1/sensors/")
	if sensorID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/sensors/{sensor_id}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteSensor(w, r, sensorID)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteSensor(w http.ResponseWriter, r *http.Request, sensorID string) {
	exists, err := s.store.SensorExists(r.Context(), sensorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "SENSOR_NOT_FOUND", "센서를 찾을 수 없습니다", "")
		return
	}
	// 센서는 readings event 만 남기고 row 만 삭제 — event 는 source-of-truth 라 보존.
	if err := s.store.DeleteSensor(r.Context(), sensorID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": sensorID})
}

// ── /v1/actuators/{id} ──────────────────────────────────────────────────────

func (s *Server) handleActuatorItem(w http.ResponseWriter, r *http.Request) {
	deviceID := extractID(r.URL.Path, "/v1/actuators/")
	if deviceID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/actuators/{device_id}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteActuator(w, r, deviceID)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteActuator(w http.ResponseWriter, r *http.Request, deviceID string) {
	exists, err := s.store.ActuatorExists(r.Context(), deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "ACTUATOR_NOT_FOUND", "장비를 찾을 수 없습니다", "")
		return
	}
	if err := s.store.DeleteActuator(r.Context(), deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deviceID})
}

// ── /v1/species-profiles/{key} ──────────────────────────────────────────────

func (s *Server) handleSpeciesProfileItem(w http.ResponseWriter, r *http.Request) {
	speciesKey := extractID(r.URL.Path, "/v1/species-profiles/")
	if speciesKey == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/species-profiles/{species}", "")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.handleDeleteSpeciesProfile(w, r, speciesKey)
	default:
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDeleteSpeciesProfile(w http.ResponseWriter, r *http.Request, key string) {
	exists, err := s.store.SpeciesProfileExists(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "SPECIES_NOT_FOUND", "어종 프로필을 찾을 수 없습니다", "")
		return
	}
	if n, err := s.store.CountTanksForSpecies(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	} else if n > 0 {
		writeError(w, http.StatusConflict, "SPECIES_IN_USE",
			"이 어종을 사용 중인 Cage/Tank 가 있어 삭제할 수 없습니다. 먼저 수조를 다른 어종으로 변경하거나 삭제하세요.", "")
		return
	}
	if err := s.store.DeleteSpeciesProfile(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": key})
}

// ── /v1/cameras/{id} 의 DELETE 분기 ─────────────────────────────────────────
// 카메라는 별도 /v1/cameras/{id} 경로가 이미 있음 (cameras.go).
// 거기에 DELETE 추가가 필요한 경우 handleCameraRoute 에서 분기.
// 본 작업에서는 controllers + camera 는 시연 외 옵션이라 명시적 별도 핸들러 없이
// /v1/cameras/{id} (handleCameraRoute) 안에 DELETE branch 만 추가됨 — cameras.go 참조.
