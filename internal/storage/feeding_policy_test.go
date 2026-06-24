package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/storage"
)

func TestGetEffectiveFeedingPolicy(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// tank 없으면 system default.
	pol, err := store.GetEffectiveFeedingPolicy(ctx, "missing_tank")
	if err != nil {
		t.Fatalf("missing tank: %v", err)
	}
	if pol.Source != "system_default" {
		t.Fatalf("expected system_default, got %s", pol.Source)
	}
	if pol.BsfMode != "standard" || pol.OperatingMode != "auto" || pol.MaxDailyCycles != 4 {
		t.Fatalf("system default mismatch: %+v", pol)
	}

	// group 에 default 설정.
	if err := store.UpsertGroupProfile(ctx, &storage.GroupProfile{
		GroupID:  "g1",
		Name:     "A동",
		Color:    "#22c55e",
		Metadata: map[string]any{"feeding_policy": map[string]any{"bsf_mode": "aggressive", "operating_mode": "manual", "max_daily_cycles": float64(6)}},
	}); err != nil {
		t.Fatalf("upsert group: %v", err)
	}

	// tank — group default 만 적용, override 없음.
	if err := store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "tank_a",
		DisplayName: "수조 A",
		Species:     "olive_flounder",
		SystemType:  "ras",
		GroupID:     "g1",
		Metadata:    map[string]any{},
	}); err != nil {
		t.Fatalf("upsert tank_a: %v", err)
	}
	pol, err = store.GetEffectiveFeedingPolicy(ctx, "tank_a")
	if err != nil {
		t.Fatalf("tank_a: %v", err)
	}
	if pol.Source != "group_default" {
		t.Fatalf("tank_a expected group_default, got %s", pol.Source)
	}
	if pol.BsfMode != "aggressive" || pol.OperatingMode != "manual" || pol.MaxDailyCycles != 6 {
		t.Fatalf("tank_a group inherit mismatch: %+v", pol)
	}

	// tank_b — override 한 필드만.
	if err := store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "tank_b",
		DisplayName: "수조 B",
		Species:     "olive_flounder",
		SystemType:  "ras",
		GroupID:     "g1",
		Metadata: map[string]any{
			"feeding_policy_override": map[string]any{"bsf_mode": "conservative"},
		},
	}); err != nil {
		t.Fatalf("upsert tank_b: %v", err)
	}
	pol, err = store.GetEffectiveFeedingPolicy(ctx, "tank_b")
	if err != nil {
		t.Fatalf("tank_b: %v", err)
	}
	if pol.Source != "tank_override" {
		t.Fatalf("tank_b expected tank_override, got %s", pol.Source)
	}
	// override 한 필드 우선, 나머지는 group default.
	if pol.BsfMode != "conservative" {
		t.Fatalf("tank_b bsf_mode: %s", pol.BsfMode)
	}
	if pol.OperatingMode != "manual" || pol.MaxDailyCycles != 6 {
		t.Fatalf("tank_b non-override fields should inherit group: %+v", pol)
	}

	// tank_c — group 없음.
	if err := store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "tank_c",
		DisplayName: "수조 C",
		Species:     "olive_flounder",
		SystemType:  "ras",
		Metadata:    map[string]any{},
	}); err != nil {
		t.Fatalf("upsert tank_c: %v", err)
	}
	pol, err = store.GetEffectiveFeedingPolicy(ctx, "tank_c")
	if err != nil {
		t.Fatalf("tank_c: %v", err)
	}
	if pol.Source != "system_default" {
		t.Fatalf("tank_c expected system_default, got %s", pol.Source)
	}
}
