package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/storage"
)

// C-13b — 액추에이터 모델 라이브러리 round-trip + FK 카운트.
func TestActuatorModelUpsertGetListDelete(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	rated := 1500.0
	cap := 20.0
	resp := 2.0
	consum := 365
	m := &storage.ActuatorModel{
		ModelID:                   "grundfos_cre_5_1",
		Vendor:                    "Grundfos",
		ProductCode:               "CRE-5-1",
		DisplayName:               "Grundfos CRE 5-1 순환 펌프",
		DeviceCategory:            "pump",
		RatedPowerW:               &rated,
		CapacityValue:             &cap,
		CapacityUnit:              "m3/h",
		ControlMethod:             "modbus",
		ResponseTimeS:             &resp,
		ConsumableReplacementDays: &consum,
		Notes:                     "RAS 순환 펌프",
	}

	if err := store.UpsertActuatorModel(ctx, m); err != nil {
		t.Fatalf("UpsertActuatorModel: %v", err)
	}

	got, err := store.GetActuatorModel(ctx, "grundfos_cre_5_1")
	if err != nil {
		t.Fatalf("GetActuatorModel: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil model")
	}
	if got.DeviceCategory != "pump" || got.RatedPowerW == nil || *got.RatedPowerW != 1500.0 {
		t.Fatalf("category+rated round-trip failed: %+v", got)
	}
	if got.ControlMethod != "modbus" {
		t.Fatalf("control_method round-trip: got %q", got.ControlMethod)
	}

	list, err := store.ListActuatorModels(ctx)
	if err != nil {
		t.Fatalf("ListActuatorModels: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 model, got %d", len(list))
	}

	// Insert a child actuator referencing the model.
	if err := store.UpsertActuator(ctx, &actuator.Actuator{
		DeviceID:      "pump_tank_01_01",
		DeviceType:    "pump",
		TankID:        "tank_01",
		ModelID:       "grundfos_cre_5_1",
		MountLocation: "tank_inlet",
		SafetyRoles:   []string{"circulation_critical"},
		OperatingMode: "auto",
	}); err != nil {
		t.Fatalf("UpsertActuator: %v", err)
	}

	// FK guard — model with child reports count > 0.
	n, err := store.CountActuatorsForModel(ctx, "grundfos_cre_5_1")
	if err != nil {
		t.Fatalf("CountActuatorsForModel: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 child instance, got %d", n)
	}

	// Round-trip via ListActuators — 새 필드 모두 확인.
	acts, err := store.ListActuators(ctx, "tank_01", "", "")
	if err != nil {
		t.Fatalf("ListActuators: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("expected 1 actuator, got %d", len(acts))
	}
	a := acts[0]
	if a.ModelID != "grundfos_cre_5_1" || a.MountLocation != "tank_inlet" || a.OperatingMode != "auto" {
		t.Fatalf("actuator new-field round-trip failed: %+v", a)
	}
	if len(a.SafetyRoles) != 1 || a.SafetyRoles[0] != "circulation_critical" {
		t.Fatalf("safety_roles round-trip: %+v", a.SafetyRoles)
	}

	// Delete child first, then model deletes successfully.
	if err := store.DeleteActuator(ctx, "pump_tank_01_01"); err != nil {
		t.Fatalf("DeleteActuator: %v", err)
	}
	if err := store.DeleteActuatorModel(ctx, "grundfos_cre_5_1"); err != nil {
		t.Fatalf("DeleteActuatorModel: %v", err)
	}
}
