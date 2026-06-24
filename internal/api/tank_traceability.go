package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// GDST first-mile 생산 CTE 핸들러: 투약 / 폐사 / 이동·선별 / 추적 타임라인.
// 모든 기록은 append-only 이벤트(source of truth)로 남고 기존 sync envelope 로 자동 전송된다.
// References: docs/49-gdst-traceability-contract.md.

func (s *Server) handleTankTraceabilityRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/treatment"):
		tankID := lifecycleTankID(r.URL.Path, "/treatment")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/treatment", "")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePostTreatment(w, r, tankID)

	case strings.HasSuffix(r.URL.Path, "/mortality"):
		tankID := lifecycleTankID(r.URL.Path, "/mortality")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/mortality", "")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePostMortality(w, r, tankID)

	case strings.HasSuffix(r.URL.Path, "/transfer"):
		tankID := lifecycleTankID(r.URL.Path, "/transfer")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/transfer", "")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePostTransfer(w, r, tankID)

	case strings.HasSuffix(r.URL.Path, "/traceability"):
		tankID := lifecycleTankID(r.URL.Path, "/traceability")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/traceability", "")
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleGetTraceability(w, r, tankID)

	case strings.HasSuffix(r.URL.Path, "/documents"):
		tankID := lifecycleTankID(r.URL.Path, "/documents")
		if tankID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/documents", "")
			return
		}
		switch r.Method {
		case http.MethodPost:
			s.handleDocumentUpload(w, r, tankID)
		case http.MethodGet:
			s.handleListDocuments(w, r, tankID)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown traceability sub-path", "")
	}
}

// activeLineage — tank 의 활성 lineage 의 stocking_id/lot_no (없으면 빈 문자열).
func (s *Server) activeLineage(r *http.Request, tankID string) (stockingID, lotNo string) {
	lc, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err != nil || lc == nil {
		return "", ""
	}
	return lc.ActiveStockingID, lc.LotNo
}

// ── POST /v1/tanks/{id}/treatment ────────────────────────────────────────────

type tankTreatmentRequest struct {
	TreatmentType   string  `json:"treatment_type"`
	Substance       string  `json:"substance"`
	Dose            float64 `json:"dose"`
	DoseUnit        string  `json:"dose_unit"`
	Reason          string  `json:"reason"`
	WithdrawalUntil string  `json:"withdrawal_until"`
	ItemID          string  `json:"item_id"`
	ConsumedQty     float64 `json:"consumed_qty"`
	AdministeredAt  string  `json:"administered_at"`
	OperatorID      string  `json:"operator_id"`
	Notes           string  `json:"notes"`
}

func (s *Server) handlePostTreatment(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankTreatmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	if req.AdministeredAt == "" {
		req.AdministeredAt = common.NowUTC().Format(time.RFC3339Nano)
	}

	stockingID, lotNo := s.activeLineage(r, tankID)
	treatmentID := common.NewID("treatment")

	payload := events.TankTreatmentRecordedPayload{
		TreatmentID:     treatmentID,
		TankID:          tankID,
		StockingID:      stockingID,
		LotNo:           lotNo,
		TreatmentType:   req.TreatmentType,
		Substance:       strings.TrimSpace(req.Substance),
		Dose:            req.Dose,
		DoseUnit:        req.DoseUnit,
		Reason:          req.Reason,
		WithdrawalUntil: req.WithdrawalUntil,
		ItemID:          req.ItemID,
		ConsumedQty:     req.ConsumedQty,
		AdministeredAt:  req.AdministeredAt,
		OperatorID:      req.OperatorID,
		Notes:           req.Notes,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "traceability", tankID,
		events.EventTankTreatmentRecorded, treatmentID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	resp := map[string]any{
		"ok":              true,
		"sequence":        seq,
		"treatment_id":    treatmentID,
		"tank_id":         tankID,
		"lot_no":          lotNo,
		"treatment_type":  req.TreatmentType,
		"administered_at": req.AdministeredAt,
	}
	// 재고 차감 — 약품 품목 지정 시. 실패는 경고만(투약 기록은 유지).
	if req.ItemID != "" && req.ConsumedQty > 0 {
		if onHand, cerr := s.consumeInventory(r.Context(), req.ItemID, req.ConsumedQty,
			"treatment", treatmentID, tankID, req.OperatorID, "", req.AdministeredAt); cerr != nil {
			resp["inventory_warning"] = cerr.Error()
		} else {
			resp["item_on_hand"] = onHand
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── POST /v1/tanks/{id}/mortality ────────────────────────────────────────────

type tankMortalityRequest struct {
	DeadCount      int    `json:"dead_count"`
	EstimatedCause string `json:"estimated_cause"`
	ObservedAt     string `json:"observed_at"`
	OperatorID     string `json:"operator_id"`
	Notes          string `json:"notes"`
}

func (s *Server) handlePostMortality(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankMortalityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	if req.ObservedAt == "" {
		req.ObservedAt = common.NowUTC().Format(time.RFC3339Nano)
	}
	if req.DeadCount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DEAD_COUNT", "dead_count must be > 0", "")
		return
	}

	stockingID, lotNo := s.activeLineage(r, tankID)
	mortalityID := common.NewID("mortality")

	payload := events.TankMortalityRecordedPayload{
		MortalityID:    mortalityID,
		TankID:         tankID,
		StockingID:     stockingID,
		LotNo:          lotNo,
		DeadCount:      req.DeadCount,
		EstimatedCause: req.EstimatedCause,
		ObservedAt:     req.ObservedAt,
		OperatorID:     req.OperatorID,
		Notes:          req.Notes,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "traceability", tankID,
		events.EventTankMortalityRecorded, mortalityID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"sequence":     seq,
		"mortality_id": mortalityID,
		"tank_id":      tankID,
		"lot_no":       lotNo,
		"dead_count":   req.DeadCount,
		"observed_at":  req.ObservedAt,
	})
}

// ── POST /v1/tanks/{id}/transfer ─────────────────────────────────────────────
// {id} = from_tank (출발). 목적지 to_tank 에 새 lineage 를 만들고 parent_lot_no 로 lineage 보존.

type tankTransferRequest struct {
	TransferType    string  `json:"transfer_type"` // move | split | merge | sale
	ToTankID        string  `json:"to_tank_id"`
	ToLotNo         string  `json:"to_lot_no"`
	DestinationName string  `json:"destination_name"` // sale 전용: 도착 농장/거래처
	VehicleInfo     string  `json:"vehicle_info"`     // sale 전용: 이동 차량
	MovedCount      int     `json:"moved_count"`
	AvgWeightG      float64 `json:"avg_weight_g"`
	TotalBiomassKg  float64 `json:"total_biomass_kg"`
	TransferredAt   string  `json:"transferred_at"`
	OperatorID      string  `json:"operator_id"`
	Notes           string  `json:"notes"`
}

func (s *Server) handlePostTransfer(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	if req.TransferredAt == "" {
		req.TransferredAt = common.NowUTC().Format(time.RFC3339Nano)
	}
	req.ToTankID = strings.TrimSpace(req.ToTankID)
	isSale := req.TransferType == "sale"
	if isSale {
		if strings.TrimSpace(req.DestinationName) == "" {
			writeError(w, http.StatusUnprocessableEntity, "MISSING_DESTINATION",
				"판매(sale) 는 destination_name(도착 농장/거래처) 이 필요합니다.", "")
			return
		}
	} else if req.ToTankID == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_TO_TANK", "to_tank_id is required", "")
		return
	}

	// 출발 lineage 확인 (활성이어야 이동/판매 가능)
	src, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if src == nil || src.Status != "active" {
		writeError(w, http.StatusConflict, "NO_ACTIVE_LIFECYCLE",
			"출발 수조에 활성 입식 기록이 없습니다. 먼저 POST /stocking 으로 입식하세요.", "")
		return
	}

	now := common.NowUTC()
	transferID := common.NewID("transfer")
	avgWeight := req.AvgWeightG
	if avgWeight <= 0 {
		avgWeight = src.InitialAvgWeightG
	}

	// 도착 lineage 정보 — 수조내 이동(move/split/merge) 에만 해당. sale 은 외부로 나가므로 없음.
	var toStockingID, toLotNo string
	if !isSale {
		toStockingID = common.NewID("stocking")
		toLotNo = strings.TrimSpace(req.ToLotNo)
		if toLotNo == "" {
			toLotNo = "LOT-" + req.ToTankID + "-" + now.Format("20060102")
		}
	}

	payload := events.TankTransferRecordedPayload{
		TransferID:      transferID,
		TransferType:    req.TransferType,
		FromTankID:      tankID,
		FromStockingID:  src.ActiveStockingID,
		FromLotNo:       src.LotNo,
		ToTankID:        req.ToTankID,
		ToStockingID:    toStockingID,
		ToLotNo:         toLotNo,
		DestinationName: strings.TrimSpace(req.DestinationName),
		VehicleInfo:     strings.TrimSpace(req.VehicleInfo),
		MovedCount:      req.MovedCount,
		AvgWeightG:      avgWeight,
		TotalBiomassKg:  req.TotalBiomassKg,
		TransferredAt:   req.TransferredAt,
		OperatorID:      req.OperatorID,
		Notes:           req.Notes,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "traceability", tankID,
		events.EventTankTransferRecorded, transferID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	// 도착 수조: 새 lineage (parent_lot_no 로 출발 lot 연결). 수조내 이동만.
	if !isSale {
		transferredAt, _ := time.Parse(time.RFC3339Nano, req.TransferredAt)
		dst := &storage.TankLifecycle{
			TankID:            req.ToTankID,
			ActiveStockingID:  toStockingID,
			Species:           src.Species,
			GrowthStage:       src.GrowthStage,
			InitialCount:      req.MovedCount,
			InitialAvgWeightG: avgWeight,
			SourceHatchery:    src.SourceHatchery,
			StockedAt:         transferredAt,
			Status:            "active",
			UpdatedAt:         now,
			LotNo:             toLotNo,
			ParentLotNo:       src.LotNo,
		}
		if err := s.store.UpsertTankLifecycle(r.Context(), dst); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
	}

	// 출발 수조: 전량 이동(move)/병합(merge)/판매(sale) 이면 마감, 분할(split) 이면 활성 유지.
	if req.TransferType == "move" || req.TransferType == "merge" || isSale {
		src.Status = "transferred"
		src.UpdatedAt = now
		if err := s.store.UpsertTankLifecycle(r.Context(), src); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		reason := "lifecycle_transferred"
		if isSale {
			reason = "lifecycle_sold"
		}
		s.forceModeOffOnLifecycleChange(r.Context(), tankID, reason,
			"이동/판매 완료 — transfer_id="+transferID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"sequence":         seq,
		"transfer_id":      transferID,
		"transfer_type":    req.TransferType,
		"from_tank_id":     tankID,
		"from_lot_no":      src.LotNo,
		"to_tank_id":       req.ToTankID,
		"to_stocking_id":   toStockingID,
		"to_lot_no":        toLotNo,
		"destination_name": req.DestinationName,
		"transferred_at":   req.TransferredAt,
	})
}

// ── GET /v1/tanks/{id}/traceability ──────────────────────────────────────────
// 활성 lot 의 생산 CTE 타임라인 (입식→사료→샘플링→투약→폐사→이동→출하).

func (s *Server) handleGetTraceability(w http.ResponseWriter, r *http.Request, tankID string) {
	lc, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	type cteItem struct {
		Type       string         `json:"type"`
		RecordedAt string         `json:"recorded_at"`
		Sequence   int64          `json:"sequence"`
		Payload    map[string]any `json:"payload"`
	}
	timeline := []cteItem{}

	// 이 tank 와 관련된 CTE 이벤트 타입들.
	cteTypes := []struct {
		eventType string
		label     string
	}{
		{events.EventTankStockingRecorded, "stocking"},
		{events.EventFeedingRecorded, "feeding"},
		{events.EventTankSamplingRecorded, "sampling"},
		{events.EventTankTreatmentRecorded, "treatment"},
		{events.EventTankMortalityRecorded, "mortality"},
		{events.EventTankTransferRecorded, "transfer"},
		{events.EventTankHarvestRecorded, "harvest"},
	}

	for _, ct := range cteTypes {
		evs, qerr := s.store.QueryEvents(r.Context(), storage.EventFilter{
			EventType: ct.eventType,
			Limit:     200,
		})
		if qerr != nil {
			continue
		}
		for _, e := range evs {
			var p map[string]any
			if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
				continue
			}
			// transfer 는 from_tank_id/to_tank_id 로, 나머지는 tank_id 로 매칭.
			if !traceMatchesTank(ct.label, p, tankID) {
				continue
			}
			timeline = append(timeline, cteItem{
				Type:       ct.label,
				RecordedAt: e.RecordedAt.Format(time.RFC3339Nano),
				Sequence:   e.Sequence,
				Payload:    p,
			})
		}
	}

	// 시간순(오래된→최신) 정렬 — CTE 흐름 표현.
	sort.Slice(timeline, func(i, j int) bool {
		if timeline[i].RecordedAt == timeline[j].RecordedAt {
			return timeline[i].Sequence < timeline[j].Sequence
		}
		return timeline[i].RecordedAt < timeline[j].RecordedAt
	})

	var currentMap any = nil
	if lc != nil {
		currentMap = lifecycleToMap(lc)
	}

	// 첨부 서류 — 타임라인 각 행이 event_ref/cte_type 으로 매칭해 표시.
	docs, _ := s.store.ListTankDocuments(r.Context(), tankID)

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":   tankID,
		"current":   currentMap,
		"timeline":  timeline,
		"count":     len(timeline),
		"documents": documentsToMaps(docs),
	})
}

// traceMatchesTank — CTE payload 가 해당 tank 와 관련 있는지.
func traceMatchesTank(label string, p map[string]any, tankID string) bool {
	if label == "transfer" {
		return p["from_tank_id"] == tankID || p["to_tank_id"] == tankID
	}
	return p["tank_id"] == tankID
}
