package storage_test

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

// down-sync 적용의 핵심 불변식: ① 활성 lifecycle count 차감 ② ledger id 멱등(재적용 무효)
// ③ 잔량 소진 시 마감 전이.
func TestApplyInboundEventReducesCountIdempotently(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := store.UpsertTankLifecycle(ctx, &storage.TankLifecycle{
		TankID:            "ras_tank_05",
		ActiveStockingID:  "stk-1",
		Species:           "olive_flounder",
		GrowthStage:       "growout",
		InitialCount:      1000,
		InitialAvgWeightG: 50,
		StockedAt:         now,
		Status:            "active",
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("seed lifecycle: %v", err)
	}

	// 1) 첫 적용: 400 마리 차감
	applied, err := store.ApplyInboundEvent(ctx, "tradesale-L1", "tank.sale_synced", "ras_tank_05", 400, "{}", "site-1", "edge-1")
	if err != nil {
		t.Fatalf("apply L1: %v", err)
	}
	if !applied {
		t.Fatal("L1 first apply: want applied=true")
	}
	lc, _ := store.GetTankLifecycle(ctx, "ras_tank_05")
	if lc.InitialCount != 600 {
		t.Fatalf("after L1: want count 600, got %d", lc.InitialCount)
	}
	if lc.Status != "active" {
		t.Fatalf("after L1: want active, got %q", lc.Status)
	}

	// 2) 같은 ledger id 재적용: 멱등(중복 차감 없음)
	applied2, err := store.ApplyInboundEvent(ctx, "tradesale-L1", "tank.sale_synced", "ras_tank_05", 400, "{}", "site-1", "edge-1")
	if err != nil {
		t.Fatalf("re-apply L1: %v", err)
	}
	if applied2 {
		t.Fatal("L1 duplicate: want applied=false")
	}
	lc2, _ := store.GetTankLifecycle(ctx, "ras_tank_05")
	if lc2.InitialCount != 600 {
		t.Fatalf("duplicate L1 changed count to %d", lc2.InitialCount)
	}

	// 3) 잔량 초과 차감(다른 ledger): 0 floor + harvested 전이
	if _, err := store.ApplyInboundEvent(ctx, "tradesale-L2", "tank.sale_synced", "ras_tank_05", 900, "{}", "site-1", "edge-1"); err != nil {
		t.Fatalf("apply L2: %v", err)
	}
	lc3, _ := store.GetTankLifecycle(ctx, "ras_tank_05")
	if lc3.InitialCount != 0 {
		t.Fatalf("after L2: want count 0, got %d", lc3.InitialCount)
	}
	if lc3.Status != "harvested" {
		t.Fatalf("after L2: want harvested, got %q", lc3.Status)
	}

	// 4) lot_ack: count 변화 없이 멱등 기록만
	a1, err := store.ApplyInboundEvent(ctx, "lotack-LOT1", "tank.lot_ack_synced", "ras_tank_05", 0, "{}", "site-1", "edge-1")
	if err != nil || !a1 {
		t.Fatalf("lot ack first: applied=%v err=%v", a1, err)
	}
	a2, _ := store.ApplyInboundEvent(ctx, "lotack-LOT1", "tank.lot_ack_synced", "ras_tank_05", 0, "{}", "site-1", "edge-1")
	if a2 {
		t.Fatal("lot ack duplicate: want applied=false")
	}
}
