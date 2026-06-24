package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/sensor"
	"bluei.kr/edge/internal/storage"
)

// C-13a — sensor_models UPSERT/Get/List/Delete + FK guard.
func TestSensorModelUpsertGetListDelete(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	rangeMin := 0.0
	rangeMax := 14.0
	acc := 0.01
	cal := 90
	m := &storage.SensorModel{
		ModelID:                 "ysi_proquatro_ph",
		Vendor:                  "YSI",
		ProductCode:             "ProQuatro-pH",
		DisplayName:             "YSI ProQuatro pH",
		MeasurementType:         "ph",
		Unit:                    "pH",
		RangeMin:                &rangeMin,
		RangeMax:                &rangeMax,
		AccuracyValue:           &acc,
		AccuracyUnit:            "pH",
		Protocol:                "rs485",
		CalibrationIntervalDays: &cal,
		WetDry:                  "wet_probe",
		Notes:                   "calibration: 2-point pH 4/7",
	}
	if err := store.UpsertSensorModel(ctx, m); err != nil {
		t.Fatalf("UpsertSensorModel: %v", err)
	}

	got, err := store.GetSensorModel(ctx, "ysi_proquatro_ph")
	if err != nil {
		t.Fatalf("GetSensorModel: %v", err)
	}
	if got == nil || got.MeasurementType != "ph" || got.Unit != "pH" || got.RangeMax == nil || *got.RangeMax != 14.0 {
		t.Fatalf("unexpected got: %+v", got)
	}

	list, err := store.ListSensorModels(ctx)
	if err != nil {
		t.Fatalf("ListSensorModels: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	// CountSensorsForModel = 0 → DELETE 가능.
	n, err := store.CountSensorsForModel(ctx, "ysi_proquatro_ph")
	if err != nil {
		t.Fatalf("CountSensorsForModel: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 sensors, got %d", n)
	}

	if err := store.DeleteSensorModel(ctx, "ysi_proquatro_ph"); err != nil {
		t.Fatalf("DeleteSensorModel: %v", err)
	}

	// 자식 연결 후 count.
	if err := store.UpsertSensorModel(ctx, m); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	depth := -0.5
	if err := store.UpsertSensor(ctx, &sensor.Sensor{
		SensorID:        "wq_test_01",
		SensorType:      "water_quality",
		TankID:          "tank_test",
		ModelID:         "ysi_proquatro_ph",
		MountLocation:   "mid_depth",
		InstalledDepthM: &depth,
		MeasurementRole: []string{"safety_gate_c3", "feeding_decision"},
	}); err != nil {
		t.Fatalf("UpsertSensor with model: %v", err)
	}
	n, err = store.CountSensorsForModel(ctx, "ysi_proquatro_ph")
	if err != nil {
		t.Fatalf("CountSensorsForModel: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 sensor, got %d", n)
	}

	// List 확인 — model_id / mount_location / measurement_role round-trip.
	got2, err := store.ListSensors(ctx, "tank_test", "", "")
	if err != nil {
		t.Fatalf("ListSensors: %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("expected 1 sensor row, got %d", len(got2))
	}
	if got2[0].ModelID != "ysi_proquatro_ph" || got2[0].MountLocation != "mid_depth" {
		t.Fatalf("unexpected sensor fields: %+v", got2[0])
	}
	if len(got2[0].MeasurementRole) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(got2[0].MeasurementRole))
	}
	if got2[0].InstalledDepthM == nil || *got2[0].InstalledDepthM != -0.5 {
		t.Fatalf("InstalledDepthM round-trip failed: %+v", got2[0].InstalledDepthM)
	}
}
