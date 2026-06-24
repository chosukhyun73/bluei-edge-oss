package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	return m
}

func inventoryToMaps(items []*storage.InventoryItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, inventoryItemToMap(it))
	}
	return out
}
