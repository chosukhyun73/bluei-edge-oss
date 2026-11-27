package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 재고관리 — 품목 마스터 + 구매(입고) + 사용(출고). on_hand = Σ구매 − Σ사용.
// References: docs/49-gdst-traceability-contract.md §재고.

// GET /v1/inventory?category=feed|drug|material
func (s *Server) handleListInventory(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category != "" && !events.ValidInventoryCategory(category) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CATEGORY", "category invalid: "+category, "")
		return
	}
	items, err := s.store.ListInventoryItems(r.Context(), category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": inventoryToMaps(items),
		"count": len(items),
	})
}

type newInventoryItemRequest struct {
	Category     string   `json:"category"`
	Name         string   `json:"name"`
	Unit         string   `json:"unit"`
	Spec         string   `json:"spec"`
	Supplier     string   `json:"supplier"`
	ReorderLevel *float64 `json:"reorder_level"`
	Notes        string   `json:"notes"`
}

// POST /v1/inventory/items — 품목 등록 (on_hand 0).
func (s *Server) handleCreateInventoryItem(w http.ResponseWriter, r *http.Request) {
	var req newInventoryItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !events.ValidInventoryCategory(req.Category) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CATEGORY", "category must be feed|drug|material", "")
		return
	}
	if req.Name == "" || strings.TrimSpace(req.Unit) == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "name, unit 필수", "")
		return
	}
	// 중복(카테고리+품목명) 방지
	if existing, _ := s.findInventoryItemByName(r.Context(), req.Category, req.Name); existing != nil {
		writeError(w, http.StatusConflict, "ITEM_EXISTS",
			"이미 등록된 품목입니다: "+req.Name+" (item_id="+existing.ItemID+")", "")
		return
	}

	now := common.NowUTC()
	item := &storage.InventoryItem{
		ItemID:       common.NewID("item"),
		Category:     req.Category,
		Name:         req.Name,
		Unit:         strings.TrimSpace(req.Unit),
		OnHandQty:    0,
		Spec:         strings.TrimSpace(req.Spec),
		Supplier:     strings.TrimSpace(req.Supplier),
		ReorderLevel: req.ReorderLevel,
		Notes:        strings.TrimSpace(req.Notes),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.UpsertInventoryItem(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": inventoryItemToMap(item)})
}

type purchaseRequest struct {
	ItemID      string  `json:"item_id"`
	Category    string  `json:"category"`
	Name        string  `json:"name"`
	Unit        string  `json:"unit"`
	Qty         float64 `json:"qty"`
	UnitPrice   float64 `json:"unit_price"`
	TotalPrice  float64 `json:"total_price"`
	Supplier    string  `json:"supplier"`
	Lot         string  `json:"lot"`
	PurchasedAt string  `json:"purchased_at"`
	OperatorID  string  `json:"operator_id"`
	Notes       string  `json:"notes"`
}

// POST /v1/inventory/purchase — 구매(입고). 품목 없으면 생성 후 +qty.
func (s *Server) handlePurchase(w http.ResponseWriter, r *http.Request) {
	var req purchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.Qty <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QTY", "qty must be > 0", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	if req.PurchasedAt == "" {
		req.PurchasedAt = common.NowUTC().Format(time.RFC3339Nano)
	}

	// 품목 해석: item_id 우선 → (category,name) 조회 → 신규 생성.
	item, err := s.resolveOrCreateItem(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "ITEM_RESOLVE_FAILED", err.Error(), "")
		return
	}

	onHand, err := s.store.AdjustInventoryOnHand(r.Context(), item.ItemID, req.Qty)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	purchaseID := common.NewID("purchase")
	payload := events.InventoryPurchaseRecordedPayload{
		PurchaseID:  purchaseID,
		ItemID:      item.ItemID,
		Category:    item.Category,
		Name:        item.Name,
		Unit:        item.Unit,
		Qty:         req.Qty,
		UnitPrice:   req.UnitPrice,
		TotalPrice:  req.TotalPrice,
		Supplier:    strings.TrimSpace(req.Supplier),
		Lot:         strings.TrimSpace(req.Lot),
		PurchasedAt: req.PurchasedAt,
		OperatorID:  req.OperatorID,
		Notes:       strings.TrimSpace(req.Notes),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "inventory", item.ItemID,
		events.EventInventoryPurchaseRecorded, purchaseID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"sequence":    seq,
		"purchase_id": purchaseID,
		"item_id":     item.ItemID,
		"name":        item.Name,
		"on_hand_qty": onHand,
		"unit":        item.Unit,
	})
}

type consumeRequest struct {
	ItemID     string  `json:"item_id"`
	Qty        float64 `json:"qty"`
	Reason     string  `json:"reason"`
	RefEvent   string  `json:"ref_event"`
	TankID     string  `json:"tank_id"`
	ConsumedAt string  `json:"consumed_at"`
	OperatorID string  `json:"operator_id"`
	Notes      string  `json:"notes"`
}

// POST /v1/inventory/consume — 사용(출고). 자재 사용/수동 차감.
func (s *Server) handleConsume(w http.ResponseWriter, r *http.Request) {
	var req consumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.Reason == "" {
		req.Reason = "material_use"
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	onHand, err := s.consumeInventory(r.Context(), req.ItemID, req.Qty, req.Reason, req.RefEvent, req.TankID, req.OperatorID, req.Notes, req.ConsumedAt)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "CONSUME_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"item_id":     req.ItemID,
		"on_hand_qty": onHand,
	})
}

// POST /v1/inventory/purchases/{purchase_id}/documents (dispatch)
func (s *Server) handleInventoryPurchasesRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/inventory/purchases/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[1] == "documents" && parts[0] != "" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleInventoryDocUpload(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/inventory/purchases/{id}/documents", "")
}

// feedComposition — 사료(품목) 영양성분표(보증성분). 모두 optional(라벨에 있는 것만).
// 값은 % 단위. 급이 AI(단백/지방 참조)·GDST 이력에 사용. [[사료 성분별 어류 관리 전략]]
type feedComposition struct {
	CrudeProteinPct    *float64 `json:"crude_protein_pct,omitempty"`    // 조단백질 ≥
	CrudeFatPct        *float64 `json:"crude_fat_pct,omitempty"`        // 조지방 ≥
	LysinePct          *float64 `json:"lysine_pct,omitempty"`           // 라이신 ≥
	TotalPhosphorusPct *float64 `json:"total_phosphorus_pct,omitempty"` // 총인 ≥
	MoisturePct        *float64 `json:"moisture_pct,omitempty"`         // 수분 ≤
	CrudeFiberPct      *float64 `json:"crude_fiber_pct,omitempty"`      // 조섬유 ≤
	CrudeAshPct        *float64 `json:"crude_ash_pct,omitempty"`        // 조회분 ≤
	CalciumPct         *float64 `json:"calcium_pct,omitempty"`          // 칼슘 ≥
	ProductModel       string   `json:"product_model,omitempty"`        // 제품모델 (예: N SK RCX 500 130A 6.0mm)
	PelletSizeMm       *float64 `json:"pellet_size_mm,omitempty"`       // 펠릿 크기(mm)
	TargetSpecies      string   `json:"target_species,omitempty"`       // 대상 어종
}

func (c *feedComposition) validate() error {
	pcts := map[string]*float64{
		"crude_protein_pct": c.CrudeProteinPct, "crude_fat_pct": c.CrudeFatPct,
		"lysine_pct": c.LysinePct, "total_phosphorus_pct": c.TotalPhosphorusPct,
		"moisture_pct": c.MoisturePct, "crude_fiber_pct": c.CrudeFiberPct,
		"crude_ash_pct": c.CrudeAshPct, "calcium_pct": c.CalciumPct,
	}
	for name, v := range pcts {
		if v != nil && (*v < 0 || *v > 100) {
			return fmt.Errorf("%s must be 0..100", name)
		}
	}
	if c.PelletSizeMm != nil && (*c.PelletSizeMm <= 0 || *c.PelletSizeMm > 50) {
		return errors.New("pellet_size_mm must be 0..50")
	}
	return nil
}

// POST /v1/inventory/items/{id}/composition — 품목 영양성분표 등록/수정.
func (s *Server) handleSetItemComposition(w http.ResponseWriter, r *http.Request, itemID string) {
	item, err := s.store.GetInventoryItem(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "inventory item not found: "+itemID, "")
		return
	}
	var comp feedComposition
	if err := json.NewDecoder(r.Body).Decode(&comp); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if err := comp.validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_COMPOSITION", err.Error(), "")
		return
	}
	comp.ProductModel = strings.TrimSpace(comp.ProductModel)
	comp.TargetSpecies = strings.TrimSpace(comp.TargetSpecies)
	b, err := json.Marshal(comp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MARSHAL_FAILED", err.Error(), "")
		return
	}
	if err := s.store.SetInventoryComposition(r.Context(), itemID, string(b)); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	item.CompositionJSON = string(b)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": inventoryItemToMap(item)})
}

// GET /v1/inventory/items/{id}/... (dispatch)
func (s *Server) handleInventoryItemsRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/inventory/items/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[1] == "composition" && parts[0] != "" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleSetItemComposition(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/inventory/items/{id}/composition", "")
}

// GET /v1/inventory/purchases?category=feed&limit=50 — 구매(입고) 이력 목록 (최신순).
func (s *Server) handleListPurchasesHistory(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category != "" && !events.ValidInventoryCategory(category) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CATEGORY", "category invalid: "+category, "")
		return
	}
	limit := 50
	if l := strings.TrimSpace(r.URL.Query().Get("limit")); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	// 카테고리는 payload 안에 있어 Go 에서 필터 → 넉넉히 조회 후 잘라낸다.
	evs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventInventoryPurchaseRecorded,
		Limit:     limit * 4,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, limit)
	for _, e := range evs {
		var p events.InventoryPurchaseRecordedPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if category != "" && p.Category != category {
			continue
		}
		out = append(out, map[string]any{
			"purchase_id":  p.PurchaseID,
			"item_id":      p.ItemID,
			"category":     p.Category,
			"name":         p.Name,
			"unit":         p.Unit,
			"qty":          p.Qty,
			"unit_price":   p.UnitPrice,
			"total_price":  p.TotalPrice,
			"supplier":     p.Supplier,
			"lot":          p.Lot,
			"purchased_at": p.PurchasedAt,
			"notes":        p.Notes,
		})
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// consumeInventory — 재고 −qty + consumption 이벤트. 사료/투약/자재사용 공통.
func (s *Server) consumeInventory(ctx context.Context, itemID string, qty float64, reason, refEvent, tankID, operator, notes, consumedAt string) (float64, error) {
	item, err := s.store.GetInventoryItem(ctx, itemID)
	if err != nil {
		return 0, err
	}
	if item == nil {
		return 0, fmt.Errorf("inventory item not found: %s", itemID)
	}
	if consumedAt == "" {
		consumedAt = common.NowUTC().Format(time.RFC3339Nano)
	}
	onHand, err := s.store.AdjustInventoryOnHand(ctx, itemID, -qty)
	if err != nil {
		return 0, err
	}
	consumptionID := common.NewID("consume")
	payload := events.InventoryConsumptionRecordedPayload{
		ConsumptionID: consumptionID,
		ItemID:        itemID,
		Category:      item.Category,
		Qty:           qty,
		Unit:          item.Unit,
		Reason:        reason,
		RefEvent:      refEvent,
		TankID:        tankID,
		ConsumedAt:    consumedAt,
		OperatorID:    operator,
		Notes:         notes,
	}
	if err := payload.Validate(); err != nil {
		return onHand, err
	}
	if _, err := s.app.AppendEvent(ctx, "api", "inventory", itemID,
		events.EventInventoryConsumptionRecorded, consumptionID, payload); err != nil {
		return onHand, err
	}
	return onHand, nil
}

func (s *Server) resolveOrCreateItem(ctx context.Context, req purchaseRequest) (*storage.InventoryItem, error) {
	if req.ItemID != "" {
		item, err := s.store.GetInventoryItem(ctx, req.ItemID)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, fmt.Errorf("inventory item not found: %s", req.ItemID)
		}
		return item, nil
	}
	if !events.ValidInventoryCategory(req.Category) {
		return nil, errors.New("category must be feed|drug|material")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || strings.TrimSpace(req.Unit) == "" {
		return nil, errors.New("name, unit 필수")
	}
	if existing, _ := s.findInventoryItemByName(ctx, req.Category, name); existing != nil {
		return existing, nil
	}
	now := common.NowUTC()
	item := &storage.InventoryItem{
		ItemID:    common.NewID("item"),
		Category:  req.Category,
		Name:      name,
		Unit:      strings.TrimSpace(req.Unit),
		OnHandQty: 0,
		Supplier:  strings.TrimSpace(req.Supplier),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.UpsertInventoryItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Server) findInventoryItemByName(ctx context.Context, category, name string) (*storage.InventoryItem, error) {
	items, err := s.store.ListInventoryItems(ctx, category)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Name == name {
			return it, nil
		}
	}
	return nil, nil
}

func inventoryItemToMap(it *storage.InventoryItem) map[string]any {
	m := map[string]any{
		"item_id":       it.ItemID,
		"category":      it.Category,
		"name":          it.Name,
		"unit":          it.Unit,
		"on_hand_qty":   it.OnHandQty,
		"spec":          it.Spec,
		"supplier":      it.Supplier,
		"notes":         it.Notes,
		"below_reorder": false,
	}
	if it.ReorderLevel != nil {
		m["reorder_level"] = *it.ReorderLevel
		m["below_reorder"] = it.OnHandQty < *it.ReorderLevel
	}
	if strings.TrimSpace(it.CompositionJSON) != "" {
		m["composition"] = json.RawMessage(it.CompositionJSON)
	} else {
		m["composition"] = nil
	}
	return m
}

func inventoryToMaps(items []*storage.InventoryItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, inventoryItemToMap(it))
	}
	return out
}
