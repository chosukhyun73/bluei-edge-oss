package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/storage"
)

func TestBroodstockCohortCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	c := &storage.BroodstockCohort{
		CohortID:     "bsc-1",
		GroupID:      "grp_broodstock",
		Species:      "olive_flounder",
		OriginType:   "wild",
		OriginRegion: "동해",
		Supplier:     "강릉수협",
		Generation:   "F0",
		MaleCount:    12,
		FemaleCount:  18,
		Maturity:     "mature",
	}
	if err := store.UpsertBroodstockCohort(ctx, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if c.CreatedAt == "" || c.UpdatedAt == "" {
		t.Fatal("expected created_at/updated_at populated")
	}

	got, err := store.GetBroodstockCohort(ctx, "bsc-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v (got=%v)", err, got)
	}
	if got.OriginType != "wild" || got.Generation != "F0" || got.FemaleCount != 18 || got.OriginRegion != "동해" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	// 다른 그룹 + 목록 격리
	if err := store.UpsertBroodstockCohort(ctx, &storage.BroodstockCohort{
		CohortID: "bsc-2", GroupID: "grp_other", Species: "rockfish", OriginType: "domestic",
	}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	list, err := store.ListBroodstockByGroup(ctx, "grp_broodstock")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].CohortID != "bsc-1" {
		t.Fatalf("list isolation failed: %+v", list)
	}

	// 갱신 멱등(created_at 보존)
	created := got.CreatedAt
	got.Maturity = "spent"
	if err := store.UpsertBroodstockCohort(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, _ := store.GetBroodstockCohort(ctx, "bsc-1")
	if again.Maturity != "spent" || again.CreatedAt != created {
		t.Fatalf("update/created_at preserve failed: %+v (created was %s)", again, created)
	}

	if err := store.DeleteBroodstockCohort(ctx, "bsc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone, _ := store.GetBroodstockCohort(ctx, "bsc-1")
	if gone != nil {
		t.Fatalf("expected nil after delete, got %+v", gone)
	}
}
