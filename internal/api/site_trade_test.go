package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSiteStockingAllocationAndHarvest — site 입식 배치→다중 수조 분배, site 출하→수조 마감.
func TestSiteStockingAllocationAndHarvest(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 거래처(부화장)
	pw := postJSONTo(t, s.handleCreatePartner, "/v1/partners", map[string]any{
		"partner_type": "hatchery", "name": "OO부화장", "site_id": "site_a",
	})
	supplierID := decodeMap(t, pw)["partner"].(map[string]any)["partner_id"].(string)

	// 입식 배치 → tank_01 2000 + tank_02 3000
	sw := postJSONTo(t, s.handleSiteStockingsCollection, "/v1/site-stockings", map[string]any{
		"site_id": "site_a", "supplier_id": supplierID,
		"species": "atlantic_salmon", "growth_stage": "growout",
		"batch_lot_no": "BATCH-001",
		"allocations": []map[string]any{
			{"tank_id": "tank_01", "count": 2000, "avg_weight_g": 50.0},
			{"tank_id": "tank_02", "count": 3000, "avg_weight_g": 50.0},
		},
	})
	if sw.Code != http.StatusOK {
		t.Fatalf("site stocking: %d %s", sw.Code, sw.Body.String())
	}
	if tc := decodeMap(t, sw)["total_count"].(float64); tc != 5000 {
		t.Fatalf("total_count = %v, want 5000", tc)
	}

	// 두 수조 lifecycle 생성 + source_hatchery 거래처명
	lc1, _ := s.store.GetTankLifecycle(ctx, "tank_01")
	lc2, _ := s.store.GetTankLifecycle(ctx, "tank_02")
	if lc1 == nil || lc1.Status != "active" || lc1.InitialCount != 2000 {
		t.Fatalf("tank_01 lifecycle wrong: %+v", lc1)
	}
	if lc2 == nil || lc2.Status != "active" || lc2.InitialCount != 3000 {
		t.Fatalf("tank_02 lifecycle wrong: %+v", lc2)
	}
	if lc1.SourceHatchery != "OO부화장" {
		t.Errorf("source_hatchery should be partner name, got %q", lc1.SourceHatchery)
	}

	// 활성 수조에 또 site 입식 분배 → 409 (사전 검증)
	dup := postJSONTo(t, s.handleSiteStockingsCollection, "/v1/site-stockings", map[string]any{
		"site_id": "site_a", "species": "x", "growth_stage": "fry",
		"allocations": []map[string]any{{"tank_id": "tank_01", "count": 1, "avg_weight_g": 1}},
	})
	if dup.Code != http.StatusConflict {
		t.Errorf("re-stock active tank expected 409, got %d", dup.Code)
	}

	// 출하 건 → tank_01 전량 마감
	hw := postJSONTo(t, s.handleSiteHarvestsCollection, "/v1/site-harvests", map[string]any{
		"site_id": "site_a",
		"lines": []map[string]any{
			{"tank_id": "tank_01", "count": 2000, "avg_weight_g": 5000.0, "full_close": true},
		},
	})
	if hw.Code != http.StatusOK {
		t.Fatalf("site harvest: %d %s", hw.Code, hw.Body.String())
	}
	lc1b, _ := s.store.GetTankLifecycle(ctx, "tank_01")
	if lc1b == nil || lc1b.Status != "harvested" {
		t.Fatalf("tank_01 should be harvested, got %+v", lc1b)
	}
	lc2b, _ := s.store.GetTankLifecycle(ctx, "tank_02")
	if lc2b == nil || lc2b.Status != "active" {
		t.Fatalf("tank_02 should remain active, got %+v", lc2b)
	}

	// site 입식/출하 목록 조회
	if lst := listSiteTrade(t, s, s.handleSiteStockingsCollection, "/v1/site-stockings?site_id=site_a", "stockings"); lst != 1 {
		t.Errorf("site stockings count = %d, want 1", lst)
	}
	if lst := listSiteTrade(t, s, s.handleSiteHarvestsCollection, "/v1/site-harvests?site_id=site_a", "harvests"); lst != 1 {
		t.Errorf("site harvests count = %d, want 1", lst)
	}
}

func listSiteTrade(t *testing.T, s *Server, h func(http.ResponseWriter, *http.Request), url, key string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	h(w, req)
	arr, _ := decodeMap(t, w)[key].([]any)
	return len(arr)
}
