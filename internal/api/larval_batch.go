package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

type larvalBatchRequest struct {
	GroupID       string  `json:"group_id"`
	TankID        string  `json:"tank_id"`
	Species       string  `json:"species"`
	SourceLotCode string  `json:"source_lot_code"`
	StartDate     string  `json:"start_date"`
	InitialCount  int     `json:"initial_count"`
	CurrentCount  int     `json:"current_count"`
	SurvivalRate  float64 `json:"survival_rate"`
	DevStage      string  `json:"dev_stage"`
	DensityPerL   float64 `json:"density_per_l"`
	Status        string  `json:"status"`
	Notes         string  `json:"notes"`
}

var larvalStatuses = map[string]bool{"rearing": true, "graduated": true, "discarded": true}

func (s *Server) handleLarvalRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListLarval(w, r)
	case http.MethodPost:
		s.handleCreateLarval(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLarvalItemRoute(w http.ResponseWriter, r *http.Request) {
	batchID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/larval-batches/"), "/")
	if batchID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/larval-batches/{batch_id}", "")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateLarval(w, r, batchID)
	case http.MethodDelete:
		s.handleDeleteLarval(w, r, batchID)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListLarval(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "group_id query param is required", "")
		return
	}
	items, err := s.store.ListLarvalBatchesByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = make([]*storage.LarvalBatch, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "items": items, "count": len(items)})
}

func (s *Server) handleCreateLarval(w http.ResponseWriter, r *http.Request) {
	var req larvalBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateLarvalRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_LARVAL_BODY", err.Error(), "")
		return
	}
	b := larvalFromRequest(req)
	b.BatchID = common.NewID("larva")
	s.snapshotLarvalPedigree(r, b)
	finalizeLarval(b)
	if err := s.store.UpsertLarvalBatch(r.Context(), b); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": b})
}

func (s *Server) handleUpdateLarval(w http.ResponseWriter, r *http.Request, batchID string) {
	existing, err := s.store.GetLarvalBatch(r.Context(), batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "BATCH_NOT_FOUND", "larval batch not found", "")
		return
	}
	var req larvalBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateLarvalRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_LARVAL_BODY", err.Error(), "")
		return
	}
	b := larvalFromRequest(req)
	b.BatchID = batchID
	b.CreatedAt = existing.CreatedAt
	s.snapshotLarvalPedigree(r, b)
	finalizeLarval(b)
	if err := s.store.UpsertLarvalBatch(r.Context(), b); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": b})
}

func (s *Server) handleDeleteLarval(w http.ResponseWriter, r *http.Request, batchID string) {
	existing, err := s.store.GetLarvalBatch(r.Context(), batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "BATCH_NOT_FOUND", "larval batch not found", "")
		return
	}
	if err := s.store.DeleteLarvalBatch(r.Context(), batchID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": batchID})
}

// snapshotLarvalPedigree copies 족보(origin_*) + species from the parent egg lot
// (spawn_batches by lot_code) — GDST lineage 연속(cohort→spawn→larval).
func (s *Server) snapshotLarvalPedigree(r *http.Request, b *storage.LarvalBatch) {
	if b.SourceLotCode == "" {
		return
	}
	sp, err := s.store.GetSpawnBatchByLotCode(r.Context(), b.SourceLotCode)
	if err != nil || sp == nil {
		return
	}
	b.OriginType = sp.OriginType
	b.OriginRegion = sp.OriginRegion
	b.Supplier = sp.Supplier
	b.Generation = sp.Generation
	if b.Species == "" {
		b.Species = sp.Species
	}
}

func finalizeLarval(b *storage.LarvalBatch) {
	if b.Status == "" {
		b.Status = "rearing"
	}
	if b.SurvivalRate == 0 && b.CurrentCount > 0 && b.InitialCount > 0 {
		b.SurvivalRate = (float64(b.CurrentCount) / float64(b.InitialCount)) * 100.0
	}
}

func validateLarvalRequest(req larvalBatchRequest) error {
	if strings.TrimSpace(req.GroupID) == "" {
		return errRequired("group_id")
	}
	if strings.TrimSpace(req.Species) == "" && strings.TrimSpace(req.SourceLotCode) == "" {
		return apiInputError("species or source_lot_code is required")
	}
	if req.InitialCount < 0 || req.CurrentCount < 0 {
		return apiInputError("counts must be >= 0")
	}
	if req.InitialCount > 0 && req.CurrentCount > req.InitialCount {
		return apiInputError("current_count cannot exceed initial_count")
	}
	if req.SurvivalRate < 0 || req.SurvivalRate > 100 {
		return apiInputError("survival_rate must be between 0 and 100")
	}
	if req.Status != "" && !larvalStatuses[req.Status] {
		return apiInputError("status must be one of rearing|graduated|discarded")
	}
	return nil
}

func larvalFromRequest(req larvalBatchRequest) *storage.LarvalBatch {
	return &storage.LarvalBatch{
		GroupID:       strings.TrimSpace(req.GroupID),
		TankID:        strings.TrimSpace(req.TankID),
		Species:       strings.TrimSpace(req.Species),
		SourceLotCode: strings.TrimSpace(req.SourceLotCode),
		StartDate:     strings.TrimSpace(req.StartDate),
		InitialCount:  req.InitialCount,
		CurrentCount:  req.CurrentCount,
		SurvivalRate:  req.SurvivalRate,
		DevStage:      strings.TrimSpace(req.DevStage),
		DensityPerL:   req.DensityPerL,
		Status:        strings.TrimSpace(req.Status),
		Notes:         strings.TrimSpace(req.Notes),
	}
}
