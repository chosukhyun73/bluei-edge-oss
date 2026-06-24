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

// 사업자(site) 단위 거래 — 입식 배치(→다중 수조 분배) / 출하 건(→다중 수조·부분 line item).
// 분배/line item 마다 기존 수조 입식/출하 코어(createTankStocking/createTankHarvest)가 파생 실행된다.
// References: docs/49-gdst-traceability-contract.md.

// ── 입식 배치 ────────────────────────────────────────────────────────────────

type siteStockingRequest struct {
	SiteID         string `json:"site_id"`
	SupplierID     string `json:"supplier_id"`
	Species        string `json:"species"`
	GrowthStage    string `json:"growth_stage"`
	SourceHatchery string `json:"source_hatchery"`
	BatchLotNo     string `json:"batch_lot_no"`
	StockedAt      string `json:"stocked_at"`
	OperatorID     string `json:"operator_id"`
	Notes          string `json:"notes"`
	Allocations    []struct {
		TankID     string  `json:"tank_id"`
		Count      int     `json:"count"`
		AvgWeightG float64 `json:"avg_weight_g"`
		LotNo      string  `json:"lot_no"`
	} `json:"allocations"`
}

func (s *Server) handleSiteStockingsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListSiteStockings(r.Context(), strings.TrimSpace(r.URL.Query().Get("site_id")))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, st := range items {
			out = append(out, siteStockingToMap(st))
		}
		writeJSON(w, http.StatusOK, map[string]any{"stockings": out, "count": len(out)})
	case http.MethodPost:
		s.handleCreateSiteStocking(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreateSiteStocking(w http.ResponseWriter, r *http.Request) {
	var req siteStockingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if strings.TrimSpace(req.SiteID) == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_SITE", "site_id is required", "")
		return
	}
	if req.Species == "" || !events.ValidGrowthStage(req.GrowthStage) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_INPUT", "species + 유효한 growth_stage 필수", "")
		return
	}
	if len(req.Allocations) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "NO_ALLOCATIONS", "allocations(수조 분배) 가 비었습니다", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	if req.StockedAt == "" {
		req.StockedAt = common.NowUTC().Format(time.RFC3339Nano)
	}

	// 사전 검증: 모든 대상 수조가 비활성이어야 부분 적용을 피한다.
	for _, a := range req.Allocations {
		if a.TankID == "" || a.Count <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_ALLOCATION", "각 분배는 tank_id + count(>0) 필요", "")
			return
		}
		lc, err := s.store.GetTankLifecycle(r.Context(), a.TankID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if lc != nil && lc.Status == "active" {
			writeError(w, http.StatusConflict, "CONFLICT_ACTIVE_LIFECYCLE",
				"수조 "+a.TankID+" 에 활성 입식이 있습니다. 먼저 출하하세요.", "")
			return
		}
	}

	supplierID := strings.TrimSpace(req.SupplierID)
	supplierName := ""
	if supplierID != "" {
		if p, _ := s.store.GetPartner(r.Context(), supplierID); p != nil {
			supplierName = p.Name
		}
	}
	if req.SourceHatchery == "" {
		req.SourceHatchery = supplierName
	}

	// 각 분배 → 수조 입식 코어 실행.
	allocations := make([]events.SiteStockingAllocation, 0, len(req.Allocations))
	totalCount := 0
	for _, a := range req.Allocations {
		lotNoIn := strings.TrimSpace(a.LotNo)
		if lotNoIn == "" {
			lotNoIn = strings.TrimSpace(req.BatchLotNo)
		}
		stockingID, lotNo, conflict, err := s.createTankStocking(r.Context(), a.TankID, tankStockingParams{
			Species:           req.Species,
			GrowthStage:       req.GrowthStage,
			InitialCount:      a.Count,
			InitialAvgWeightG: a.AvgWeightG,
			SourceHatchery:    req.SourceHatchery,
			SupplierID:        supplierID,
			LotNo:             lotNoIn,
			StockedAt:         req.StockedAt,
			OperatorID:        req.OperatorID,
		})
		if conflict || err != nil {
			msg := "수조 " + a.TankID + " 입식 실패"
			if err != nil {
				msg += ": " + err.Error()
			}
			writeError(w, http.StatusUnprocessableEntity, "ALLOCATION_FAILED", msg, "")
			return
		}
		allocations = append(allocations, events.SiteStockingAllocation{
			TankID: a.TankID, Count: a.Count, AvgWeightG: a.AvgWeightG, LotNo: lotNo, StockingID: stockingID,
		})
		totalCount += a.Count
	}

	siteStockingID := common.NewID("sstk")
	now := common.NowUTC()
	payload := events.SiteStockingRecordedPayload{
		SiteStockingID: siteStockingID,
		SiteID:         req.SiteID,
		SupplierID:     supplierID,
		SupplierName:   supplierName,
		Species:        req.Species,
		GrowthStage:    req.GrowthStage,
		SourceHatchery: req.SourceHatchery,
		BatchLotNo:     strings.TrimSpace(req.BatchLotNo),
		TotalCount:     totalCount,
		Allocations:    allocations,
		StockedAt:      req.StockedAt,
		OperatorID:     req.OperatorID,
		Notes:          strings.TrimSpace(req.Notes),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "site_trade", req.SiteID,
		events.EventSiteStockingRecorded, siteStockingID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	allocJSON, _ := json.Marshal(allocations)
	stockedAt, _ := time.Parse(time.RFC3339Nano, req.StockedAt)
	st := &storage.SiteStocking{
		SiteStockingID:  siteStockingID,
		SiteID:          req.SiteID,
		SupplierID:      supplierID,
		SupplierName:    supplierName,
		Species:         req.Species,
		GrowthStage:     req.GrowthStage,
		SourceHatchery:  req.SourceHatchery,
		BatchLotNo:      strings.TrimSpace(req.BatchLotNo),
		TotalCount:      totalCount,
		AllocationsJSON: string(allocJSON),
		StockedAt:       stockedAt,
		OperatorID:      req.OperatorID,
		Notes:           strings.TrimSpace(req.Notes),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.store.UpsertSiteStocking(r.Context(), st); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"sequence":         seq,
		"site_stocking_id": siteStockingID,
		"total_count":      totalCount,
		"allocations":      allocations,
	})
}

// ── 출하 건 ──────────────────────────────────────────────────────────────────

type siteHarvestRequest struct {
	SiteID      string `json:"site_id"`
	BuyerID     string `json:"buyer_id"`
	VehicleInfo string `json:"vehicle_info"`
	HarvestedAt string `json:"harvested_at"`
	OperatorID  string `json:"operator_id"`
	Notes       string `json:"notes"`
	Lines       []struct {
		TankID     string  `json:"tank_id"`
		LotNo      string  `json:"lot_no"`
		Count      int     `json:"count"`
		AvgWeightG float64 `json:"avg_weight_g"`
		FullClose  bool    `json:"full_close"`
	} `json:"lines"`
}

func (s *Server) handleSiteHarvestsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListSiteHarvests(r.Context(), strings.TrimSpace(r.URL.Query().Get("site_id")))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, h := range items {
			out = append(out, siteHarvestToMap(h))
		}
		writeJSON(w, http.StatusOK, map[string]any{"harvests": out, "count": len(out)})
	case http.MethodPost:
		s.handleCreateSiteHarvest(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreateSiteHarvest(w http.ResponseWriter, r *http.Request) {
	var req siteHarvestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if strings.TrimSpace(req.SiteID) == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_SITE", "site_id is required", "")
		return
	}
	if len(req.Lines) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "NO_LINES", "lines(수조 출하 내역) 가 비었습니다", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	if req.HarvestedAt == "" {
		req.HarvestedAt = common.NowUTC().Format(time.RFC3339Nano)
	}

	// 사전 검증: full_close 대상 수조는 활성이어야 함.
	for _, l := range req.Lines {
		if l.TankID == "" || l.Count <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_LINE", "각 line 은 tank_id + count(>0) 필요", "")
			return
		}
		if l.FullClose {
			lc, err := s.store.GetTankLifecycle(r.Context(), l.TankID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
				return
			}
			if lc == nil || lc.Status != "active" {
				writeError(w, http.StatusConflict, "NO_ACTIVE_LIFECYCLE",
					"수조 "+l.TankID+" 에 활성 입식이 없어 전량마감 불가", "")
				return
			}
		}
	}

	buyerID := strings.TrimSpace(req.BuyerID)
	buyerName := ""
	if buyerID != "" {
		if p, _ := s.store.GetPartner(r.Context(), buyerID); p != nil {
			buyerName = p.Name
		}
	}

	lines := make([]events.SiteHarvestLine, 0, len(req.Lines))
	totalCount := 0
	for _, l := range req.Lines {
		line := events.SiteHarvestLine{
			TankID: l.TankID, LotNo: l.LotNo, Count: l.Count, AvgWeightG: l.AvgWeightG, FullClose: l.FullClose,
		}
		if l.FullClose {
			harvestID, noActive, err := s.createTankHarvest(r.Context(), l.TankID, tankHarvestParams{
				HarvestedCount: l.Count,
				AvgWeightG:     l.AvgWeightG,
				HarvestedAt:    req.HarvestedAt,
				OperatorID:     req.OperatorID,
				Notes:          req.Notes,
			})
			if noActive || err != nil {
				msg := "수조 " + l.TankID + " 출하 실패"
				if err != nil {
					msg += ": " + err.Error()
				}
				writeError(w, http.StatusUnprocessableEntity, "HARVEST_LINE_FAILED", msg, "")
				return
			}
			line.HarvestID = harvestID
		}
		lines = append(lines, line)
		totalCount += l.Count
	}

	siteHarvestID := common.NewID("shrv")
	now := common.NowUTC()
	payload := events.SiteHarvestRecordedPayload{
		SiteHarvestID: siteHarvestID,
		SiteID:        req.SiteID,
		BuyerID:       buyerID,
		BuyerName:     buyerName,
		TotalCount:    totalCount,
		Lines:         lines,
		VehicleInfo:   strings.TrimSpace(req.VehicleInfo),
		HarvestedAt:   req.HarvestedAt,
		OperatorID:    req.OperatorID,
		Notes:         strings.TrimSpace(req.Notes),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "site_trade", req.SiteID,
		events.EventSiteHarvestRecorded, siteHarvestID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	linesJSON, _ := json.Marshal(lines)
	harvestedAt, _ := time.Parse(time.RFC3339Nano, req.HarvestedAt)
	h := &storage.SiteHarvest{
		SiteHarvestID: siteHarvestID,
		SiteID:        req.SiteID,
		BuyerID:       buyerID,
		BuyerName:     buyerName,
		TotalCount:    totalCount,
		LinesJSON:     string(linesJSON),
		VehicleInfo:   strings.TrimSpace(req.VehicleInfo),
		HarvestedAt:   harvestedAt,
		OperatorID:    req.OperatorID,
		Notes:         strings.TrimSpace(req.Notes),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.UpsertSiteHarvest(r.Context(), h); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"sequence":        seq,
		"site_harvest_id": siteHarvestID,
		"total_count":     totalCount,
		"lines":           lines,
	})
}

// ── 거래 서류 (subject = site_stocking | site_harvest) ────────────────────────

func (s *Server) handleSiteStockingItemRoute(w http.ResponseWriter, r *http.Request) {
	s.siteTradeDocRoute(w, r, "/v1/site-stockings/", "site_stocking")
}

func (s *Server) handleSiteHarvestItemRoute(w http.ResponseWriter, r *http.Request) {
	s.siteTradeDocRoute(w, r, "/v1/site-harvests/", "site_harvest")
}

func (s *Server) siteTradeDocRoute(w http.ResponseWriter, r *http.Request, prefix, subjectType string) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "documents" && parts[0] != "" {
		switch r.Method {
		case http.MethodPost:
			s.uploadSiteTradeDoc(w, r, subjectType, parts[0])
		case http.MethodGet:
			docs, err := s.store.ListDocumentsBySubject(r.Context(), subjectType, parts[0])
			if err != nil {
				writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"documents": documentsToMaps(docs), "count": len(docs)})
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "expected "+prefix+"{id}/documents", "")
}

func (s *Server) uploadSiteTradeDoc(w http.ResponseWriter, r *http.Request, subjectType, subjectID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocBytes+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", err.Error(), "")
		return
	}
	docType := strings.TrimSpace(r.FormValue("doc_type"))
	if !events.ValidDocType(docType) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DOC_TYPE", "doc_type invalid: "+docType, "")
		return
	}
	blob, ok := s.saveDocBlob(w, r, "documents/_"+subjectType+"/"+subjectID)
	if !ok {
		return
	}
	operator := strings.TrimSpace(r.FormValue("operator_id"))
	if operator == "" {
		operator = "operator"
	}
	doc := storage.TankDocument{
		DocumentID:  blob.DocID,
		CTEType:     "other",
		DocType:     docType,
		EventRef:    subjectID,
		Filename:    blob.Filename,
		MimeType:    blob.Mime,
		SizeBytes:   blob.Size,
		SHA256:      blob.SHA,
		StoredPath:  blob.RelPath,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
		UploadedBy:  operator,
		UploadedAt:  common.NowUTC(),
		SubjectType: subjectType,
		SubjectID:   subjectID,
	}
	seq, ok := s.recordDocument(w, r, blob, doc)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "sequence": seq, "document_id": blob.DocID,
		"subject_id": subjectID, "doc_type": docType, "download_url": "/v1/documents/" + blob.DocID,
	})
}

// ── maps ─────────────────────────────────────────────────────────────────────

func siteStockingToMap(st *storage.SiteStocking) map[string]any {
	var alloc any
	_ = json.Unmarshal([]byte(st.AllocationsJSON), &alloc)
	return map[string]any{
		"site_stocking_id": st.SiteStockingID,
		"site_id":          st.SiteID,
		"supplier_id":      st.SupplierID,
		"supplier_name":    st.SupplierName,
		"species":          st.Species,
		"growth_stage":     st.GrowthStage,
		"source_hatchery":  st.SourceHatchery,
		"batch_lot_no":     st.BatchLotNo,
		"total_count":      st.TotalCount,
		"allocations":      alloc,
		"stocked_at":       fmtAPITime(st.StockedAt),
	}
}

func siteHarvestToMap(h *storage.SiteHarvest) map[string]any {
	var lines any
	_ = json.Unmarshal([]byte(h.LinesJSON), &lines)
	return map[string]any{
		"site_harvest_id": h.SiteHarvestID,
		"site_id":         h.SiteID,
		"buyer_id":        h.BuyerID,
		"buyer_name":      h.BuyerName,
		"total_count":     h.TotalCount,
		"lines":           lines,
		"vehicle_info":    h.VehicleInfo,
		"harvested_at":    fmtAPITime(h.HarvestedAt),
	}
}
