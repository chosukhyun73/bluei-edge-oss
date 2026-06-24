package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// ── 라우트 디스패치 ──────────────────────────────────────────────────────────

// handleTankLifecycleRoute — /stocking | /harvest | /lifecycle 디스패치.
func (s *Server) handleTankLifecycleRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/stocking"):
		tankID := lifecycleTankID(r.URL.Path, "/stocking")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/stocking", "")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePostStocking(w, r, tankID)

	case strings.HasSuffix(r.URL.Path, "/harvest"):
		tankID := lifecycleTankID(r.URL.Path, "/harvest")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/harvest", "")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePostHarvest(w, r, tankID)

	case strings.HasSuffix(r.URL.Path, "/lifecycle"):
		tankID := lifecycleTankID(r.URL.Path, "/lifecycle")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/lifecycle", "")
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleGetLifecycle(w, r, tankID)

	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown lifecycle sub-path", "")
	}
}

func lifecycleTankID(path, suffix string) string {
	trimmed := strings.TrimSuffix(path, suffix)
	if !strings.HasPrefix(trimmed, "/v1/tanks/") {
		return ""
	}
	id := strings.TrimPrefix(trimmed, "/v1/tanks/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

// ── POST /v1/tanks/{id}/stocking ─────────────────────────────────────────────

type tankStockingRequest struct {
	LotNo                 string  `json:"lot_no"`
	SupplierID            string  `json:"supplier_id"`
	Species               string  `json:"species"`
	GrowthStage           string  `json:"growth_stage"`
	InitialCount          int     `json:"initial_count"`
	InitialAvgWeightG     float64 `json:"initial_avg_weight_g"`
	InitialTotalBiomassKg float64 `json:"initial_total_biomass_kg"`
	TargetHarvestWeightG  float64 `json:"target_harvest_weight_g"`
	TargetHarvestDate     string  `json:"target_harvest_date"`
	SourceHatchery        string  `json:"source_hatchery"`
	StockedAt             string  `json:"stocked_at"`
	OperatorID            string  `json:"operator_id"`
}

func (s *Server) handlePostStocking(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankStockingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	// 필수 필드 검증
	if req.Species == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_SPECIES", "species is required", "")
		return
	}
	if !events.ValidGrowthStage(req.GrowthStage) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GROWTH_STAGE",
			"growth_stage must be one of: fry|juvenile|growout|broodstock", "")
		return
	}
	if req.InitialCount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_INITIAL_COUNT",
			"initial_count must be > 0", "")
		return
	}
	if req.InitialAvgWeightG <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_INITIAL_AVG_WEIGHT",
			"initial_avg_weight_g must be > 0", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}

	stockingID, lotNo, conflict, err := s.createTankStocking(r.Context(), tankID, tankStockingParams{
		Species:               req.Species,
		GrowthStage:           req.GrowthStage,
		InitialCount:          req.InitialCount,
		InitialAvgWeightG:     req.InitialAvgWeightG,
		InitialTotalBiomassKg: req.InitialTotalBiomassKg,
		TargetHarvestWeightG:  req.TargetHarvestWeightG,
		TargetHarvestDate:     req.TargetHarvestDate,
		SourceHatchery:        req.SourceHatchery,
		SupplierID:            strings.TrimSpace(req.SupplierID),
		LotNo:                 req.LotNo,
		StockedAt:             req.StockedAt,
		OperatorID:            req.OperatorID,
	})
	if conflict {
		writeError(w, http.StatusConflict, "CONFLICT_ACTIVE_LIFECYCLE", err.Error(), "")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "STOCKING_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"stocking_id":          stockingID,
		"lot_no":               lotNo,
		"tank_id":              tankID,
		"species":              req.Species,
		"growth_stage":         req.GrowthStage,
		"initial_count":        req.InitialCount,
		"initial_avg_weight_g": req.InitialAvgWeightG,
	})
}

// tankStockingParams — 수조 입식 코어 입력. site 입식 분배에서도 재사용.
type tankStockingParams struct {
	Species               string
	GrowthStage           string
	InitialCount          int
	InitialAvgWeightG     float64
	InitialTotalBiomassKg float64
	TargetHarvestWeightG  float64
	TargetHarvestDate     string
	SourceHatchery        string
	SupplierID            string
	LotNo                 string
	StockedAt             string
	OperatorID            string
}

// createTankStocking — 수조 입식 코어(이벤트+lifecycle+모드off+profile). 활성 lineage 면 conflict=true.
func (s *Server) createTankStocking(ctx context.Context, tankID string, p tankStockingParams) (stockingID, lotNo string, conflict bool, err error) {
	if p.OperatorID == "" {
		p.OperatorID = "operator"
	}
	existing, err := s.store.GetTankLifecycle(ctx, tankID)
	if err != nil {
		return "", "", false, err
	}
	if existing != nil && existing.Status == "active" {
		return "", "", true, fmt.Errorf("수조 %s 에 활성 입식이 있습니다 (stocking_id=%s). 먼저 출하하세요.", tankID, existing.ActiveStockingID)
	}
	now := common.NowUTC()
	if p.StockedAt == "" {
		p.StockedAt = now.Format(time.RFC3339Nano)
	}
	lotNo = strings.TrimSpace(p.LotNo)
	if lotNo == "" {
		day := now
		if t, e := time.Parse(time.RFC3339Nano, p.StockedAt); e == nil {
			day = t
		}
		lotNo = "LOT-" + tankID + "-" + day.Format("20060102")
	}
	biomass := p.InitialTotalBiomassKg
	if biomass == 0 {
		biomass = float64(p.InitialCount) * p.InitialAvgWeightG / 1000.0
	}
	sourceHatchery := p.SourceHatchery
	if p.SupplierID != "" && sourceHatchery == "" {
		if pr, _ := s.store.GetPartner(ctx, p.SupplierID); pr != nil {
			sourceHatchery = pr.Name
		}
	}
	stockingID = common.NewID("stocking")
	payload := events.TankStockingRecordedPayload{
		StockingID:            stockingID,
		TankID:                tankID,
		LotNo:                 lotNo,
		Species:               p.Species,
		GrowthStage:           p.GrowthStage,
		InitialCount:          p.InitialCount,
		InitialAvgWeightG:     p.InitialAvgWeightG,
		InitialTotalBiomassKg: biomass,
		TargetHarvestWeightG:  p.TargetHarvestWeightG,
		TargetHarvestDate:     p.TargetHarvestDate,
		SourceHatchery:        sourceHatchery,
		SupplierID:            p.SupplierID,
		StockedAt:             p.StockedAt,
		OperatorID:            p.OperatorID,
	}
	if err := payload.Validate(); err != nil {
		return "", "", false, err
	}
	if _, err := s.app.AppendEvent(ctx, "api", "lifecycle", tankID,
		events.EventTankStockingRecorded, stockingID, payload); err != nil {
		return "", "", false, err
	}
	stockedAt, _ := time.Parse(time.RFC3339Nano, p.StockedAt)
	lc := &storage.TankLifecycle{
		TankID:            tankID,
		ActiveStockingID:  stockingID,
		Species:           p.Species,
		GrowthStage:       p.GrowthStage,
		InitialCount:      p.InitialCount,
		InitialAvgWeightG: p.InitialAvgWeightG,
		TargetHarvestDate: p.TargetHarvestDate,
		SourceHatchery:    sourceHatchery,
		StockedAt:         stockedAt,
		Status:            "active",
		UpdatedAt:         now,
		LotNo:             lotNo,
	}
	if p.TargetHarvestWeightG > 0 {
		v := p.TargetHarvestWeightG
		lc.TargetHarvestWeightG = &v
	}
	if err := s.store.UpsertTankLifecycle(ctx, lc); err != nil {
		return "", "", false, err
	}
	s.forceModeOffOnLifecycleChange(ctx, tankID, "lifecycle_stocking_started",
		"입식 등록됨 — stocking_id="+stockingID)
	s.updateTankProfileFromStocking(ctx, tankID, p.Species, p.InitialCount, p.InitialAvgWeightG, biomass)
	return stockingID, lotNo, false, nil
}

// updateTankProfileFromStocking — 기존 TankProfile 도 입식 정보로 갱신 (legacy 경로 호환).
func (s *Server) updateTankProfileFromStocking(ctx context.Context, tankID, species string, count int, avgWeight, biomass float64) {
	profile, err := s.store.GetTankProfile(ctx, tankID)
	if err != nil || profile == nil {
		return // best-effort
	}
	profile.Species = species
	profile.FishCount = count
	profile.AvgWeightG = avgWeight
	profile.BiomassKg = biomass
	_ = s.store.UpsertTankProfile(ctx, profile)
}

// ── POST /v1/tanks/{id}/harvest ──────────────────────────────────────────────

type tankHarvestRequest struct {
	HarvestedCount int     `json:"harvested_count"`
	AvgWeightG     float64 `json:"avg_weight_g"`
	TotalBiomassKg float64 `json:"total_biomass_kg"`
	CycleFCR       float64 `json:"cycle_fcr"`
	HarvestedAt    string  `json:"harvested_at"`
	OperatorID     string  `json:"operator_id"`
	Notes          string  `json:"notes"`
}

func (s *Server) handlePostHarvest(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankHarvestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	if req.HarvestedCount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_HARVESTED_COUNT",
			"harvested_count must be > 0", "")
		return
	}
	if req.AvgWeightG <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_AVG_WEIGHT",
			"avg_weight_g must be > 0", "")
		return
	}

	harvestID, noActive, err := s.createTankHarvest(r.Context(), tankID, tankHarvestParams{
		HarvestedCount: req.HarvestedCount,
		AvgWeightG:     req.AvgWeightG,
		TotalBiomassKg: req.TotalBiomassKg,
		CycleFCR:       req.CycleFCR,
		HarvestedAt:    req.HarvestedAt,
		OperatorID:     req.OperatorID,
		Notes:          req.Notes,
	})
	if noActive {
		writeError(w, http.StatusConflict, "NO_ACTIVE_LIFECYCLE",
			"활성 입식 기록이 없습니다. 먼저 입식하세요.", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "HARVEST_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"harvest_id":   harvestID,
		"tank_id":      tankID,
		"harvested_at": req.HarvestedAt,
	})
}

// tankHarvestParams — 수조 출하 코어 입력. site 출하 line item(full_close)에서도 재사용.
type tankHarvestParams struct {
	HarvestedCount int
	AvgWeightG     float64
	TotalBiomassKg float64
	CycleFCR       float64
	HarvestedAt    string
	OperatorID     string
	Notes          string
}

// createTankHarvest — 수조 출하 코어. 활성 lineage 없으면 noActive=true. lifecycle 을 harvested 로 마감.
func (s *Server) createTankHarvest(ctx context.Context, tankID string, p tankHarvestParams) (harvestID string, noActive bool, err error) {
	if p.OperatorID == "" {
		p.OperatorID = "operator"
	}
	lc, err := s.store.GetTankLifecycle(ctx, tankID)
	if err != nil {
		return "", false, err
	}
	if lc == nil || lc.Status != "active" {
		return "", true, fmt.Errorf("수조 %s 에 활성 입식이 없습니다", tankID)
	}
	now := common.NowUTC()
	if p.HarvestedAt == "" {
		p.HarvestedAt = now.Format(time.RFC3339Nano)
	}
	if p.AvgWeightG <= 0 {
		p.AvgWeightG = lc.InitialAvgWeightG // fallback (site line item에 평균체중 없을 때)
	}
	harvestID = common.NewID("harvest")
	payload := events.TankHarvestRecordedPayload{
		HarvestID:      harvestID,
		StockingID:     lc.ActiveStockingID,
		LotNo:          lc.LotNo,
		TankID:         tankID,
		HarvestedCount: p.HarvestedCount,
		AvgWeightG:     p.AvgWeightG,
		TotalBiomassKg: p.TotalBiomassKg,
		CycleFCR:       p.CycleFCR,
		HarvestedAt:    p.HarvestedAt,
		OperatorID:     p.OperatorID,
		Notes:          p.Notes,
	}
	if err := payload.Validate(); err != nil {
		return "", false, err
	}
	if _, err := s.app.AppendEvent(ctx, "api", "lifecycle", tankID,
		events.EventTankHarvestRecorded, harvestID, payload); err != nil {
		return "", false, err
	}
	lc.Status = "harvested"
	lc.UpdatedAt = now
	if err := s.store.UpsertTankLifecycle(ctx, lc); err != nil {
		return "", false, err
	}
	s.forceModeOffOnLifecycleChange(ctx, tankID, "lifecycle_harvested",
		"출하 완료 — harvest_id="+harvestID)
	return harvestID, false, nil
}

// ── GET /v1/tanks/{id}/lifecycle ─────────────────────────────────────────────

func (s *Server) handleGetLifecycle(w http.ResponseWriter, r *http.Request, tankID string) {
	lc, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 최근 stocking + harvest 이벤트 history 수집
	stockingEvts, _ := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankStockingRecorded,
		Limit:     50,
	})
	harvestEvts, _ := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankHarvestRecorded,
		Limit:     50,
	})

	type histItem struct {
		Type       string         `json:"type"`
		RecordedAt string         `json:"recorded_at"`
		Payload    map[string]any `json:"payload"`
	}
	var history []histItem

	for _, e := range stockingEvts {
		var p map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p["tank_id"] != tankID {
			continue
		}
		history = append(history, histItem{
			Type:       "stocking",
			RecordedAt: e.RecordedAt.Format(time.RFC3339Nano),
			Payload:    p,
		})
	}
	for _, e := range harvestEvts {
		var p map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p["tank_id"] != tankID {
			continue
		}
		history = append(history, histItem{
			Type:       "harvest",
			RecordedAt: e.RecordedAt.Format(time.RFC3339Nano),
			Payload:    p,
		})
	}

	// 최신 우선 정렬
	sort.Slice(history, func(i, j int) bool {
		return history[i].RecordedAt > history[j].RecordedAt
	})

	var currentMap any = nil
	if lc != nil {
		currentMap = lifecycleToMap(lc)
	}

	if history == nil {
		history = []histItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id": tankID,
		"current": currentMap,
		"history": history,
	})
}

func lifecycleToMap(lc *storage.TankLifecycle) map[string]any {
	m := map[string]any{
		"tank_id":              lc.TankID,
		"active_stocking_id":   lc.ActiveStockingID,
		"lot_no":               lc.LotNo,
		"parent_lot_no":        lc.ParentLotNo,
		"species":              lc.Species,
		"growth_stage":         lc.GrowthStage,
		"initial_count":        lc.InitialCount,
		"initial_avg_weight_g": lc.InitialAvgWeightG,
		"target_harvest_date":  lc.TargetHarvestDate,
		"source_hatchery":      lc.SourceHatchery,
		"stocked_at":           fmtAPITime(lc.StockedAt),
		"status":               lc.Status,
		"updated_at":           fmtAPITime(lc.UpdatedAt),
	}
	if lc.TargetHarvestWeightG != nil {
		m["target_harvest_weight_g"] = *lc.TargetHarvestWeightG
	}
	return m
}

// ── Auto-mode-off helper ─────────────────────────────────────────────────────

// forceModeOffOnLifecycleChange — 입식/출하 시 자율 모드를 off 로 강제.
// 이미 off 이면 noop. 기존 EventTankAutonomousModeChanged 이벤트 재사용.
func (s *Server) forceModeOffOnLifecycleChange(ctx context.Context, tankID, reason, detail string) {
	row, err := s.store.GetTankAutonomousMode(ctx, tankID)
	if err != nil {
		return
	}
	if row == nil || row.Mode == "off" {
		return // 이미 off
	}
	prev := row.Mode
	now := common.NowUTC()
	row.Mode = "off"
	row.Reason = reason + ": " + detail
	row.ChangedAt = now
	row.ChangedBy = "system"
	if err := s.store.UpsertTankAutonomousMode(ctx, row); err != nil {
		return
	}
	ev := events.TankAutonomousModeChangedPayload{
		TankID:       tankID,
		PreviousMode: prev,
		NewMode:      "off",
		OperatorID:   "system",
		Reason:       reason,
		ChangedAt:    now.Format(time.RFC3339Nano),
	}
	if err := ev.Validate(); err != nil {
		return // best-effort
	}
	_, _ = s.app.AppendEvent(ctx, "api", "lifecycle", tankID,
		events.EventTankAutonomousModeChanged, common.NewID("mode"), ev)
}
