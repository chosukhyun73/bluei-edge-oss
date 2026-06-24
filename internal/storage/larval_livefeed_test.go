package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/storage"
)

func TestLarvalBatchCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	b := &storage.LarvalBatch{
		BatchID: "larva-1", GroupID: "larval_a", Species: "atlantic_salmon",
		SourceLotCode: "EGG-ABC123", OriginType: "wild", Generation: "F0",
		InitialCount: 9000, CurrentCount: 8100, SurvivalRate: 90, DevStage: "first_feeding",
	}
	if err := store.UpsertLarvalBatch(ctx, b); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if b.Status != "rearing" {
		t.Fatalf("default status: %q", b.Status)
	}
	got, err := store.GetLarvalBatch(ctx, "larva-1")
	if err != nil || got == nil || got.CurrentCount != 8100 || got.OriginType != "wild" || got.SourceLotCode != "EGG-ABC123" {
		t.Fatalf("roundtrip: %v %+v", err, got)
	}
	if err := store.UpsertLarvalBatch(ctx, &storage.LarvalBatch{BatchID: "larva-2", GroupID: "other", Species: "x"}); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	list, _ := store.ListLarvalBatchesByGroup(ctx, "larval_a")
	if len(list) != 1 || list[0].BatchID != "larva-1" {
		t.Fatalf("list isolation: %+v", list)
	}
	if err := store.DeleteLarvalBatch(ctx, "larva-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g, _ := store.GetLarvalBatch(ctx, "larva-1"); g != nil {
		t.Fatalf("expected nil after delete")
	}
}

func TestLiveFeedCultureCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	c := &storage.LiveFeedCulture{
		CultureID: "lfc-1", GroupID: "larval_a", FeedType: "rotifer",
		Strain: "L-type", VolumeL: 500, DensityPerML: 250, Status: "culturing",
	}
	if err := store.UpsertLiveFeedCulture(ctx, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := store.GetLiveFeedCulture(ctx, "lfc-1")
	if err != nil || got == nil || got.FeedType != "rotifer" || got.DensityPerML != 250 || got.Strain != "L-type" {
		t.Fatalf("roundtrip: %v %+v", err, got)
	}
	list, _ := store.ListLiveFeedByGroup(ctx, "larval_a")
	if len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if err := store.DeleteLiveFeedCulture(ctx, "lfc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if g, _ := store.GetLiveFeedCulture(ctx, "lfc-1"); g != nil {
		t.Fatalf("expected nil after delete")
	}
}
