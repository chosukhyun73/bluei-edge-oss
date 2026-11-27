package api

import (
	"context"
	"net/http"
	"testing"

	"bluei.kr/edge/internal/storage"
)

// 마릿수 정본 = 입식 + 합치기 유입 − 폐사 − 분조 유출 − 부분출하 (이벤트 집계).
// 플랫폼이 GX10 마릿수를 받기 시작하기 전에 맞아야 한다 (docs/53 §4 0단계).

func seedProfiles(t *testing.T, s *Server, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := s.store.UpsertTankProfile(context.Background(), &storage.TankProfile{
			TankID: id, DisplayName: id, Species: "연어", AvgWeightG: 50,
		}); err != nil {
			t.Fatalf("upsert profile %s: %v", id, err)
		}
	}
}

func fishCount(t *testing.T, s *Server, id string) int {
	t.Helper()
	tp, err := s.store.GetTankProfile(context.Background(), id)
	if err != nil || tp == nil {
		t.Fatalf("profile %s: %v", id, err)
	}
	return tp.FishCount
}

// 분조(split): 원 수조는 활성 유지 + 옮긴 만큼 줄어야 한다.
func TestSplitReducesSourceCount(t *testing.T) {
	s := newTestServerWithApp(t)
	seedProfiles(t, s, "tank_01", "tank_02")
	if w := postStocking(t, s, "tank_01", defaultStockingBody()); w.Code != http.StatusOK {
		t.Fatalf("stocking: %d %s", w.Code, w.Body.String())
	}
	if w := postTrace(t, s, "tank_01", "/mortality", map[string]any{"dead_count": 10, "operator_id": "op1"}); w.Code != http.StatusOK {
		t.Fatalf("mortality: %d %s", w.Code, w.Body.String())
	}
	w := postTrace(t, s, "tank_01", "/transfer", map[string]any{
		"transfer_type": "split", "to_tank_id": "tank_02", "moved_count": 300, "operator_id": "op1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("split: %d %s", w.Code, w.Body.String())
	}
	if got := fishCount(t, s, "tank_01"); got != 690 { // 1000 − 10 − 300
		t.Errorf("source after split = %d, want 690", got)
	}
	if got := fishCount(t, s, "tank_02"); got != 300 {
		t.Errorf("dest after split = %d, want 300", got)
	}
	lc, _ := s.store.GetTankLifecycle(context.Background(), "tank_01")
	if lc == nil || lc.Status != "active" {
		t.Errorf("split source should stay active, got %+v", lc)
	}
}

// 합치기(merge): 받는 수조에 이미 입식이 있으면 그 lot 을 유지하고 마릿수를 더한다(덮어쓰기 금지).
func TestMergeIntoOccupiedTankAddsCount(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	seedProfiles(t, s, "tank_01", "tank_02")
	b1 := defaultStockingBody()
	b1["lot_no"] = "LOT-A"
	b2 := defaultStockingBody()
	b2["lot_no"] = "LOT-B"
	b2["initial_count"] = 400
	postStocking(t, s, "tank_01", b1)
	postStocking(t, s, "tank_02", b2)
	before, _ := s.store.GetTankLifecycle(ctx, "tank_02")

	w := postTrace(t, s, "tank_01", "/transfer", map[string]any{
		"transfer_type": "merge", "to_tank_id": "tank_02", "moved_count": 1000, "operator_id": "op1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("merge: %d %s", w.Code, w.Body.String())
	}
	after, _ := s.store.GetTankLifecycle(ctx, "tank_02")
	if after == nil || after.LotNo != "LOT-B" || after.ActiveStockingID != before.ActiveStockingID {
		t.Fatalf("merge must keep destination lineage, got %+v", after)
	}
	if got := fishCount(t, s, "tank_02"); got != 1400 {
		t.Errorf("dest after merge = %d, want 1400", got)
	}
	src, _ := s.store.GetTankLifecycle(ctx, "tank_01")
	if src == nil || src.Status != "transferred" {
		t.Errorf("merge source should be closed, got %+v", src)
	}
}

// 이동/분조는 이미 입식된 수조로 갈 수 없다(그건 합치기) → 409, 목적지 그대로.
func TestMoveIntoOccupiedTankRejected(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	seedProfiles(t, s, "tank_01", "tank_02")
	postStocking(t, s, "tank_01", defaultStockingBody())
	b2 := defaultStockingBody()
	b2["lot_no"] = "LOT-B"
	postStocking(t, s, "tank_02", b2)

	for _, typ := range []string{"move", "split"} {
		w := postTrace(t, s, "tank_01", "/transfer", map[string]any{
			"transfer_type": typ, "to_tank_id": "tank_02", "moved_count": 10, "operator_id": "op1",
		})
		if w.Code != http.StatusConflict {
			t.Errorf("%s into occupied tank expected 409, got %d %s", typ, w.Code, w.Body.String())
		}
	}
	lc, _ := s.store.GetTankLifecycle(ctx, "tank_02")
	if lc == nil || lc.LotNo != "LOT-B" {
		t.Errorf("destination must be untouched, got %+v", lc)
	}
}

// 사업자 출하 부분 라인(full_close=false)도 그 수조 마릿수를 줄여야 한다.
func TestSitePartialHarvestReducesCount(t *testing.T) {
	s := newTestServerWithApp(t)
	seedProfiles(t, s, "tank_01")
	postStocking(t, s, "tank_01", defaultStockingBody())

	hw := postJSONTo(t, s.handleSiteHarvestsCollection, "/v1/site-harvests", map[string]any{
		"site_id": "site_a",
		"lines":   []map[string]any{{"tank_id": "tank_01", "count": 250, "avg_weight_g": 900.0, "full_close": false}},
	})
	if hw.Code != http.StatusOK {
		t.Fatalf("site harvest: %d %s", hw.Code, hw.Body.String())
	}
	if got := fishCount(t, s, "tank_01"); got != 750 {
		t.Errorf("after partial harvest = %d, want 750", got)
	}
}
