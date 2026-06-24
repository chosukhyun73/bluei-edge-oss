package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

type spawnBatchRequest struct {
	GroupID           string  `json:"group_id"`
	TankID            string  `json:"tank_id"`
	Species           string  `json:"species"`
	FemaleCohortID    string  `json:"female_cohort_id"`
	MaleCohortID      string  `json:"male_cohort_id"`
	SpawnDate         string  `json:"spawn_date"`
	EggCount          int     `json:"egg_count"`
	EggVolumeML       float64 `json:"egg_volume_ml"`
	FertilizationRate float64 `json:"fertilization_rate"`
	HatchDate         string  `json:"hatch_date"`
	HatchedCount      int     `json:"hatched_count"`
	HatchRate         float64 `json:"hatch_rate"`
	Status            string  `json:"status"`
	Buyer             string  `json:"buyer"`
	Notes             string  `json:"notes"`
}

var spawnStatuses = map[string]bool{
	"incubating": true, "hatched": true, "discarded": true, "sold": true,
}

func (s *Server) handleSpawnBatchRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSpawnBatches(w, r)
	case http.MethodPost:
		s.handleCreateSpawnBatch(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSpawnBatchItemRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/spawn-batches/"), "/")
	parts := strings.Split(rel, "/")
	batchID := parts[0]
	if batchID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/spawn-batches/{batch_id}", "")
		return
	}

	// /v1/spawn-batches/{id}/publish — 플랫폼 발행(canonical SEEDSTOCK lot UP 브릿지).
	if len(parts) > 1 && parts[1] == "publish" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePublishSpawnBatch(w, r, batchID)
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.handleUpdateSpawnBatch(w, r, batchID)
	case http.MethodDelete:
		s.handleDeleteSpawnBatch(w, r, batchID)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePublishSpawnBatch pushes the egg/seed lot to the platform as a canonical
// SEEDSTOCK lot (족보 KDE 포함) via the sync rail — landBased/GDST 연결. 즉시 전송이라
// 엔드포인트가 닿아야 성공(오프라인이면 재시도). 백엔드는 owner+lot_code 멱등.
func (s *Server) handlePublishSpawnBatch(w http.ResponseWriter, r *http.Request, batchID string) {
	b, err := s.store.GetSpawnBatch(r.Context(), batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if b == nil {
		writeError(w, http.StatusNotFound, "BATCH_NOT_FOUND", "spawn batch not found", "")
		return
	}
	if b.LotCode == "" {
		writeError(w, http.StatusUnprocessableEntity, "NO_LOT_CODE", "batch has no lot_code", "")
		return
	}

	payload := map[string]any{
		"group_id":      b.GroupID,
		"lot_code":      b.LotCode,
		"species":       b.Species,
		"origin_type":   b.OriginType,
		"origin_region": b.OriginRegion,
		"supplier":      b.Supplier,
		"generation":    b.Generation,
		"egg_count":     b.EggCount,
		"hatched_count": b.HatchedCount,
		"spawn_date":    b.SpawnDate,
		"hatch_date":    b.HatchDate,
	}
	resp, err := s.sync.PushSeedLot(r.Context(), payload)
	if err != nil {
		writeError(w, http.StatusBadGateway, "PLATFORM_SYNC_FAILED",
			"플랫폼 발행 전송 실패(엔드포인트 미연결?): "+err.Error(), "")
		return
	}

	if b.Metadata == nil {
		b.Metadata = map[string]any{}
	}
	b.Metadata["published"] = true
	b.Metadata["published_at"] = common.NowUTC().Format(time.RFC3339Nano)
	if err := s.store.UpsertSpawnBatch(r.Context(), b); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "item": b, "projected": resp.Projected, "lot_code": b.LotCode,
	})
}

func (s *Server) handleListSpawnBatches(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "group_id query param is required", "")
		return
	}
	items, err := s.store.ListSpawnBatchesByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = make([]*storage.SpawnBatch, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "items": items, "count": len(items)})
}

func (s *Server) handleCreateSpawnBatch(w http.ResponseWriter, r *http.Request) {
	var req spawnBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateSpawnBatchRequest(req, true); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SPAWN_BODY", err.Error(), "")
		return
	}
	b := spawnFromRequest(req)
	b.BatchID = common.NewID("spawn")
	b.LotCode = eggLotCode(b.BatchID)
	s.snapshotPedigree(r, b)
	finalizeSpawn(b)
	if err := s.store.UpsertSpawnBatch(r.Context(), b); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": b})
}

func (s *Server) handleUpdateSpawnBatch(w http.ResponseWriter, r *http.Request, batchID string) {
	existing, err := s.store.GetSpawnBatch(r.Context(), batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "BATCH_NOT_FOUND", "spawn batch not found", "")
		return
	}
	var req spawnBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateSpawnBatchRequest(req, false); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SPAWN_BODY", err.Error(), "")
		return
	}
	b := spawnFromRequest(req)
	b.BatchID = batchID
	b.CreatedAt = existing.CreatedAt
	b.LotCode = existing.LotCode // lot_code 불변(추적/QR 주체)
	s.snapshotPedigree(r, b)
	finalizeSpawn(b)
	if err := s.store.UpsertSpawnBatch(r.Context(), b); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": b})
}

func (s *Server) handleDeleteSpawnBatch(w http.ResponseWriter, r *http.Request, batchID string) {
	existing, err := s.store.GetSpawnBatch(r.Context(), batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "BATCH_NOT_FOUND", "spawn batch not found", "")
		return
	}
	if err := s.store.DeleteSpawnBatch(r.Context(), batchID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": batchID})
}

// snapshotPedigree copies broodstock 족보(origin_type/region/supplier/generation) from the
// female cohort (fallback male) into the batch — GDST sourceOfBroodstock lineage.
func (s *Server) snapshotPedigree(r *http.Request, b *storage.SpawnBatch) {
	cohortID := b.FemaleCohortID
	if cohortID == "" {
		cohortID = b.MaleCohortID
	}
	if cohortID == "" {
		return
	}
	c, err := s.store.GetBroodstockCohort(r.Context(), cohortID)
	if err != nil || c == nil {
		return
	}
	b.OriginType = c.OriginType
	b.OriginRegion = c.OriginRegion
	b.Supplier = c.Supplier
	b.Generation = c.Generation
}

// finalizeSpawn fills derived fields — 부화율 자동(미입력 시 hatched/egg).
func finalizeSpawn(b *storage.SpawnBatch) {
	if b.Status == "" {
		b.Status = "incubating"
	}
	if b.HatchRate == 0 && b.HatchedCount > 0 && b.EggCount > 0 {
		b.HatchRate = (float64(b.HatchedCount) / float64(b.EggCount)) * 100.0
	}
}

func validateSpawnBatchRequest(req spawnBatchRequest, isCreate bool) error {
	if strings.TrimSpace(req.GroupID) == "" {
		return errRequired("group_id")
	}
	if strings.TrimSpace(req.Species) == "" {
		return errRequired("species")
	}
	// 알 규칙(기본 sanity) — 개수/용량 음수 금지, 생성 시 최소 1개 측정 필요.
	if req.EggCount < 0 || req.EggVolumeML < 0 {
		return apiInputError("egg_count/egg_volume_ml must be >= 0")
	}
	if isCreate && req.EggCount == 0 && req.EggVolumeML == 0 {
		return apiInputError("egg_count or egg_volume_ml is required")
	}
	if req.HatchedCount < 0 {
		return apiInputError("hatched_count must be >= 0")
	}
	if req.EggCount > 0 && req.HatchedCount > req.EggCount {
		return apiInputError("hatched_count cannot exceed egg_count")
	}
	for _, v := range []float64{req.FertilizationRate, req.HatchRate} {
		if v < 0 || v > 100 {
			return apiInputError("rates must be between 0 and 100")
		}
	}
	if req.Status != "" && !spawnStatuses[req.Status] {
		return apiInputError("status must be one of incubating|hatched|discarded|sold")
	}
	return nil
}

func spawnFromRequest(req spawnBatchRequest) *storage.SpawnBatch {
	return &storage.SpawnBatch{
		GroupID:           strings.TrimSpace(req.GroupID),
		TankID:            strings.TrimSpace(req.TankID),
		Species:           strings.TrimSpace(req.Species),
		FemaleCohortID:    strings.TrimSpace(req.FemaleCohortID),
		MaleCohortID:      strings.TrimSpace(req.MaleCohortID),
		SpawnDate:         strings.TrimSpace(req.SpawnDate),
		EggCount:          req.EggCount,
		EggVolumeML:       req.EggVolumeML,
		FertilizationRate: req.FertilizationRate,
		HatchDate:         strings.TrimSpace(req.HatchDate),
		HatchedCount:      req.HatchedCount,
		HatchRate:         req.HatchRate,
		Status:            strings.TrimSpace(req.Status),
		Buyer:             strings.TrimSpace(req.Buyer),
		Notes:             strings.TrimSpace(req.Notes),
	}
}

// eggLotCode derives a readable egg lot code from the batch id ULID suffix.
func eggLotCode(batchID string) string {
	suffix := batchID
	if i := strings.LastIndex(batchID, "_"); i >= 0 {
		suffix = batchID[i+1:]
	}
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	return "EGG-" + strings.ToUpper(suffix)
}
