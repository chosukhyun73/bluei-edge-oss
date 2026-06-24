package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postJSONTo(t *testing.T, h func(http.ResponseWriter, *http.Request), path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	return m
}

// TestInventoryNetting — 구매(+) − 급이/투약/자재사용(−) = 정확한 on_hand.
func TestInventoryNetting(t *testing.T) {
	s := newTestServerWithApp(t)

	// 1) 사료 품목 등록
	w := postJSONTo(t, s.handleCreateInventoryItem, "/v1/inventory/items", map[string]any{
		"category": "feed", "name": "사료A", "unit": "kg", "reorder_level": 10.0,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create item: %d %s", w.Code, w.Body.String())
	}
	item := decodeMap(t, w)["item"].(map[string]any)
	feedItemID := item["item_id"].(string)

	// 2) 구매 100kg → on_hand 100
	w = postJSONTo(t, s.handlePurchase, "/v1/inventory/purchase", map[string]any{
		"item_id": feedItemID, "qty": 100.0, "supplier": "OO사료", "unit_price": 1200,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("purchase: %d %s", w.Code, w.Body.String())
	}
	if oh := decodeMap(t, w)["on_hand_qty"].(float64); oh != 100 {
		t.Fatalf("on_hand after purchase = %v, want 100", oh)
	}

	// 3) 급이 — item 차감 2kg → 98
	fw := postJSONTo(t, s.handlePostFeedingRecord, "/v1/feedings", map[string]any{
		"tank_id": "tank_01", "feed_amount_g": 2000, "source": "manual",
		"item_id": feedItemID, "consumed_qty": 2.0,
	})
	if fw.Code != http.StatusCreated {
		t.Fatalf("feeding: %d %s", fw.Code, fw.Body.String())
	}
	if oh, ok := decodeMap(t, fw)["item_on_hand"].(float64); !ok || oh != 98 {
		t.Fatalf("on_hand after feeding = %v, want 98 (resp=%s)", oh, fw.Body.String())
	}

	// 4) 목록 조회로 재확인
	lreq := httptest.NewRequest(http.MethodGet, "/v1/inventory?category=feed", nil)
	lw := httptest.NewRecorder()
	s.handleListInventory(lw, lreq)
	items := decodeMap(t, lw)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["on_hand_qty"].(float64) != 98 {
		t.Fatalf("list on_hand mismatch: %s", lw.Body.String())
	}

	// 5) 약품 품목 + 구매 5병 + 투약 1병 차감 → 4
	w = postJSONTo(t, s.handleCreateInventoryItem, "/v1/inventory/items", map[string]any{
		"category": "drug", "name": "옥시테트라사이클린", "unit": "병",
	})
	drugItemID := decodeMap(t, w)["item"].(map[string]any)["item_id"].(string)
	postJSONTo(t, s.handlePurchase, "/v1/inventory/purchase", map[string]any{"item_id": drugItemID, "qty": 5.0})
	tw := postTrace(t, s, "tank_01", "/treatment", map[string]any{
		"treatment_type": "antibiotic", "substance": "oxytetracycline", "operator_id": "op1",
		"item_id": drugItemID, "consumed_qty": 1.0,
	})
	if tw.Code != http.StatusOK {
		t.Fatalf("treatment: %d %s", tw.Code, tw.Body.String())
	}
	if oh, ok := decodeMap(t, tw)["item_on_hand"].(float64); !ok || oh != 4 {
		t.Fatalf("drug on_hand after treatment = %v, want 4 (resp=%s)", oh, tw.Body.String())
	}

	// 6) 자재 품목 + 구매 50 + 자재사용 5 → 45
	w = postJSONTo(t, s.handleCreateInventoryItem, "/v1/inventory/items", map[string]any{
		"category": "material", "name": "그물망", "unit": "ea",
	})
	matItemID := decodeMap(t, w)["item"].(map[string]any)["item_id"].(string)
	postJSONTo(t, s.handlePurchase, "/v1/inventory/purchase", map[string]any{"item_id": matItemID, "qty": 50.0})
	cw := postJSONTo(t, s.handleConsume, "/v1/inventory/consume", map[string]any{
		"item_id": matItemID, "qty": 5.0, "reason": "material_use",
	})
	if cw.Code != http.StatusOK {
		t.Fatalf("consume: %d %s", cw.Code, cw.Body.String())
	}
	if oh := decodeMap(t, cw)["on_hand_qty"].(float64); oh != 45 {
		t.Fatalf("material on_hand = %v, want 45", oh)
	}
}

// TestPurchaseAutoCreatesItem — item_id 없이 (category,name,unit) 구매 시 품목 자동 생성.
func TestPurchaseAutoCreatesItem(t *testing.T) {
	s := newTestServerWithApp(t)
	w := postJSONTo(t, s.handlePurchase, "/v1/inventory/purchase", map[string]any{
		"category": "feed", "name": "사료B", "unit": "포대", "qty": 20.0,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("purchase auto-create: %d %s", w.Code, w.Body.String())
	}
	resp := decodeMap(t, w)
	if resp["item_id"].(string) == "" || resp["on_hand_qty"].(float64) != 20 {
		t.Fatalf("auto-create purchase wrong: %s", w.Body.String())
	}
	// 같은 품목 재구매 → 누적(40), 새 품목 생성 안 함
	w2 := postJSONTo(t, s.handlePurchase, "/v1/inventory/purchase", map[string]any{
		"category": "feed", "name": "사료B", "unit": "포대", "qty": 20.0,
	})
	if oh := decodeMap(t, w2)["on_hand_qty"].(float64); oh != 40 {
		t.Fatalf("re-purchase should accumulate to 40, got %v", oh)
	}
}
