package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/storage"
)

func TestSpawnBatchCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	b := &storage.SpawnBatch{
		BatchID:        "spawn-1",
		GroupID:        "spawning_a",
		Species:        "atlantic_salmon",
		LotCode:        "EGG-ABC123",
		FemaleCohortID: "bsc-1",
		OriginType:     "wild",
		OriginRegion:   "동해",
		Generation:     "F0",
		SpawnDate:      "2026-06-15",
		EggCount:       10000,
		EggVolumeML:    1200.5,
	}
	if err := store.UpsertSpawnBatch(ctx, b); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if b.Status != "incubating" {
		t.Fatalf("expected default status incubating, got %q", b.Status)
	}

	got, err := store.GetSpawnBatch(ctx, "spawn-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v (got=%v)", err, got)
	}
	if got.EggCount != 10000 || got.EggVolumeML != 1200.5 || got.OriginType != "wild" || got.LotCode != "EGG-ABC123" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	// 부화 결과 갱신(같은 레코드) + 그룹 격리
	got.HatchedCount = 8500
	got.HatchRate = 85
	got.HatchDate = "2026-06-30"
	got.Status = "hatched"
	created := got.CreatedAt
	if err := store.UpsertSpawnBatch(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, _ := store.GetSpawnBatch(ctx, "spawn-1")
	if again.Status != "hatched" || again.HatchedCount != 8500 || again.CreatedAt != created {
		t.Fatalf("hatch update/created_at failed: %+v", again)
	}

	if err := store.UpsertSpawnBatch(ctx, &storage.SpawnBatch{
		BatchID: "spawn-2", GroupID: "other", Species: "rockfish", EggCount: 5,
	}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	list, err := store.ListSpawnBatchesByGroup(ctx, "spawning_a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].BatchID != "spawn-1" {
		t.Fatalf("list isolation failed: %+v", list)
	}

	if err := store.DeleteSpawnBatch(ctx, "spawn-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gone, _ := store.GetSpawnBatch(ctx, "spawn-1"); gone != nil {
		t.Fatalf("expected nil after delete, got %+v", gone)
	}
}
