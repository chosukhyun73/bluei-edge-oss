package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/storage"
)

func TestGroupUpsertGetListDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// 빈 상태에서 조회 → 빈 slice
	groups, err := store.ListGroupProfiles(ctx)
	if err != nil {
		t.Fatalf("ListGroupProfiles empty: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}

	// 존재하지 않는 group → nil
	got, err := store.GetGroupProfile(ctx, "g_missing")
	if err != nil {
		t.Fatalf("GetGroupProfile missing: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}

	// upsert g1
	g1 := &storage.GroupProfile{
		GroupID:     "g1",
		Name:        "A동 순환시스템",
		Description: "광어 전문 양식 그룹",
		Color:       "#22c55e",
		Metadata:    map[string]any{"zone": "A"},
	}
	if err := store.UpsertGroupProfile(ctx, g1); err != nil {
		t.Fatalf("UpsertGroupProfile g1: %v", err)
	}

	// upsert g2
	g2 := &storage.GroupProfile{
		GroupID:     "g2",
		Name:        "B동 해상시스템",
		Description: "참돔 양식 그룹",
		Color:       "#3b82f6",
		Metadata:    map[string]any{},
	}
	if err := store.UpsertGroupProfile(ctx, g2); err != nil {
		t.Fatalf("UpsertGroupProfile g2: %v", err)
	}

	// list → 2개
	all, err := store.ListGroupProfiles(ctx)
	if err != nil {
		t.Fatalf("ListGroupProfiles: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(all))
	}

	// get g1
	row, err := store.GetGroupProfile(ctx, "g1")
	if err != nil {
		t.Fatalf("GetGroupProfile g1: %v", err)
	}
	if row == nil {
		t.Fatal("expected g1, got nil")
	}
	if row.Name != "A동 순환시스템" {
		t.Errorf("name: got %q", row.Name)
	}
	if row.Color != "#22c55e" {
		t.Errorf("color: got %q", row.Color)
	}

	// upsert 업데이트 (동일 group_id 로 덮어쓰기)
	g1.Name = "A동 순환시스템 (수정)"
	if err := store.UpsertGroupProfile(ctx, g1); err != nil {
		t.Fatalf("UpsertGroupProfile g1 update: %v", err)
	}
	updated, _ := store.GetGroupProfile(ctx, "g1")
	if updated.Name != "A동 순환시스템 (수정)" {
		t.Errorf("update: expected modified name, got %q", updated.Name)
	}

	// delete g2
	if err := store.DeleteGroupProfile(ctx, "g2"); err != nil {
		t.Fatalf("DeleteGroupProfile g2: %v", err)
	}
	gone, err := store.GetGroupProfile(ctx, "g2")
	if err != nil {
		t.Fatalf("GetGroupProfile after delete: %v", err)
	}
	if gone != nil {
		t.Fatalf("expected nil after delete, got %+v", gone)
	}

	// list → 1개
	remaining, err := store.ListGroupProfiles(ctx)
	if err != nil {
		t.Fatalf("ListGroupProfiles after delete: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 group after delete, got %d", len(remaining))
	}
}

func TestListTanksByGroup(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// 두 그룹 생성
	for _, g := range []*storage.GroupProfile{
		{GroupID: "g1", Name: "A동", Color: "#22c55e", Metadata: map[string]any{}},
		{GroupID: "g2", Name: "B동", Color: "#3b82f6", Metadata: map[string]any{}},
	} {
		if err := store.UpsertGroupProfile(ctx, g); err != nil {
			t.Fatalf("UpsertGroupProfile %s: %v", g.GroupID, err)
		}
	}

	// 4개 Cage/Tank 생성: t1/t2 → g1, t3/t4 → g2
	tanks := []*storage.TankProfile{
		{TankID: "t1", DisplayName: "Tank 1", Species: "halibut", SystemType: "ras", GroupID: "g1"},
		{TankID: "t2", DisplayName: "Tank 2", Species: "halibut", SystemType: "ras", GroupID: "g1"},
		{TankID: "t3", DisplayName: "Tank 3", Species: "red_sea_bream", SystemType: "sea_cage", GroupID: "g2"},
		{TankID: "t4", DisplayName: "Tank 4", Species: "red_sea_bream", SystemType: "sea_cage", GroupID: "g2"},
	}
	for _, tp := range tanks {
		if err := store.UpsertTankProfile(ctx, tp); err != nil {
			t.Fatalf("UpsertTankProfile %s: %v", tp.TankID, err)
		}
	}

	// g1 → 2개
	g1Tanks, err := store.ListTanksByGroup(ctx, "g1")
	if err != nil {
		t.Fatalf("ListTanksByGroup g1: %v", err)
	}
	if len(g1Tanks) != 2 {
		t.Fatalf("expected 2 tanks in g1, got %d", len(g1Tanks))
	}
	for _, tp := range g1Tanks {
		if tp.GroupID != "g1" {
			t.Errorf("expected group_id g1, got %q", tp.GroupID)
		}
	}

	// g2 → 2개
	g2Tanks, err := store.ListTanksByGroup(ctx, "g2")
	if err != nil {
		t.Fatalf("ListTanksByGroup g2: %v", err)
	}
	if len(g2Tanks) != 2 {
		t.Fatalf("expected 2 tanks in g2, got %d", len(g2Tanks))
	}

	// 존재하지 않는 group → 빈 slice (행 없음, 오류 아님)
	none, err := store.ListTanksByGroup(ctx, "g_missing")
	if err != nil {
		t.Fatalf("ListTanksByGroup missing: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected 0 tanks for missing group, got %d", len(none))
	}
}
