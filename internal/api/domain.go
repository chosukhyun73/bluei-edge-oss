package api

import (
	"net/http"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/sensor"
	"bluei.kr/edge/internal/wtg"
)

// handleGetFarms serves GET /v1/farms.
func (s *Server) handleGetFarms(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListFarms(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*farm.Farm{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleGetSites serves GET /v1/sites?farm_id=.
func (s *Server) handleGetSites(w http.ResponseWriter, r *http.Request) {
	farmID := r.URL.Query().Get("farm_id")
	items, err := s.store.ListSites(r.Context(), farmID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleGetWTGs serves GET /v1/water-treatment-groups?site_id=.
func (s *Server) handleGetWTGs(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("site_id")
	items, err := s.store.ListWTGs(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*wtg.Group{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleGetActuators serves GET /v1/actuators?tank_id=&site_id=&wtg_id=.
func (s *Server) handleGetActuators(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := s.store.ListActuators(r.Context(), q.Get("tank_id"), q.Get("site_id"), q.Get("wtg_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*actuator.Actuator{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleGetSensors serves GET /v1/sensors?tank_id=&site_id=&wtg_id=.
func (s *Server) handleGetSensors(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := s.store.ListSensors(r.Context(), q.Get("tank_id"), q.Get("site_id"), q.Get("wtg_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*sensor.Sensor{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleGetSpeciesProfiles serves GET /v1/species-profiles.
func (s *Server) handleGetSpeciesProfiles(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSpeciesProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
