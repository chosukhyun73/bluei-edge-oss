package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

type broodstockRequest struct {
	GroupID        string `json:"group_id"`
	TankID         string `json:"tank_id"`
	Species        string `json:"species"`
	OriginType     string `json:"origin_type"` // wild | domestic
	OriginRegion   string `json:"origin_region"`
	Supplier       string `json:"supplier"`
	Generation     string `json:"generation"`
	ParentCohortID string `json:"parent_cohort_id"`
	AcquiredDate   string `json:"acquired_date"`
	MaleCount      int    `json:"male_count"`
	FemaleCount    int    `json:"female_count"`
	Maturity       string `json:"maturity"`
	Notes          string `json:"notes"`
}

// handleBroodstockRoute — /v1/broodstock (collection).
func (s *Server) handleBroodstockRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListBroodstock(w, r)
	case http.MethodPost:
		s.handleCreateBroodstock(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBroodstockItemRoute — /v1/broodstock/{cohort_id}.
func (s *Server) handleBroodstockItemRoute(w http.ResponseWriter, r *http.Request) {
	cohortID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/broodstock/"), "/")
	if cohortID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/broodstock/{cohort_id}", "")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateBroodstock(w, r, cohortID)
	case http.MethodDelete:
		s.handleDeleteBroodstock(w, r, cohortID)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListBroodstock(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "group_id query param is required", "")
		return
	}
	items, err := s.store.ListBroodstockByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = make([]*storage.BroodstockCohort, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group_id": groupID,
		"items":    items,
		"count":    len(items),
	})
}

func (s *Server) handleCreateBroodstock(w http.ResponseWriter, r *http.Request) {
	var req broodstockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateBroodstockRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_BROODSTOCK_BODY", err.Error(), "")
		return
	}
	c := broodstockFromRequest(req)
	c.CohortID = common.NewID("bsc")
	if err := s.store.UpsertBroodstockCohort(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": c})
}

func (s *Server) handleUpdateBroodstock(w http.ResponseWriter, r *http.Request, cohortID string) {
	existing, err := s.store.GetBroodstockCohort(r.Context(), cohortID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "COHORT_NOT_FOUND", "broodstock cohort not found", "")
		return
	}
	var req broodstockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateBroodstockRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_BROODSTOCK_BODY", err.Error(), "")
		return
	}
	c := broodstockFromRequest(req)
	c.CohortID = cohortID            // path 우선
	c.CreatedAt = existing.CreatedAt // 생성시각 보존
	if err := s.store.UpsertBroodstockCohort(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": c})
}

func (s *Server) handleDeleteBroodstock(w http.ResponseWriter, r *http.Request, cohortID string) {
	existing, err := s.store.GetBroodstockCohort(r.Context(), cohortID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "COHORT_NOT_FOUND", "broodstock cohort not found", "")
		return
	}
	if err := s.store.DeleteBroodstockCohort(r.Context(), cohortID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": cohortID})
}

func validateBroodstockRequest(req broodstockRequest) error {
	if strings.TrimSpace(req.GroupID) == "" {
		return errRequired("group_id")
	}
	if strings.TrimSpace(req.Species) == "" {
		return errRequired("species")
	}
	if req.OriginType != "wild" && req.OriginType != "domestic" {
		return apiInputError("origin_type must be 'wild' or 'domestic'")
	}
	if req.MaleCount < 0 || req.FemaleCount < 0 {
		return apiInputError("male_count/female_count must be >= 0")
	}
	return nil
}

func broodstockFromRequest(req broodstockRequest) *storage.BroodstockCohort {
	return &storage.BroodstockCohort{
		GroupID:        strings.TrimSpace(req.GroupID),
		TankID:         strings.TrimSpace(req.TankID),
		Species:        strings.TrimSpace(req.Species),
		OriginType:     req.OriginType,
		OriginRegion:   strings.TrimSpace(req.OriginRegion),
		Supplier:       strings.TrimSpace(req.Supplier),
		Generation:     strings.TrimSpace(req.Generation),
		ParentCohortID: strings.TrimSpace(req.ParentCohortID),
		AcquiredDate:   strings.TrimSpace(req.AcquiredDate),
		MaleCount:      req.MaleCount,
		FemaleCount:    req.FemaleCount,
		Maturity:       strings.TrimSpace(req.Maturity),
		Notes:          strings.TrimSpace(req.Notes),
	}
}
