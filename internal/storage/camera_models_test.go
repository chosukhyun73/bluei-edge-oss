package storage_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/storage"
)

// C-11 — 카메라 모델 라이브러리 round-trip.
// dual lens 모델 + 인스턴스 model_id link 검증.
func TestCameraModelUpsertGetListDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	baseline := 60.0
	w, h := 1920, 1080
	fov := 95.0
	fps := 30
	m := &storage.CameraModel{
		ModelID:     "stereolabs_zed2i",
		Vendor:      "Stereolabs",
		ProductCode: "ZED2i",
		DisplayName: "ZED 2i Stereo",
		LensType:    "dual",
		BaselineMM:  &baseline,
		ResolutionW: &w,
		ResolutionH: &h,
		FOVDeg:      &fov,
		FPS:         &fps,
		NightMode:   false,
		Protocols:   []string{"usb", "ethernet"},
		Notes:       "dual lens stereo — size measurement",
	}

	if err := store.UpsertCameraModel(ctx, m); err != nil {
		t.Fatalf("UpsertCameraModel: %v", err)
	}

	got, err := store.GetCameraModel(ctx, "stereolabs_zed2i")
	if err != nil {
		t.Fatalf("GetCameraModel: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil model")
	}
	if got.LensType != "dual" || got.BaselineMM == nil || *got.BaselineMM != 60.0 {
		t.Fatalf("dual+baseline round-trip failed: %+v", got)
	}
	if len(got.Protocols) != 2 {
		t.Fatalf("protocols round-trip: got %+v", got.Protocols)
	}

	list, err := store.ListCameraModels(ctx)
	if err != nil {
		t.Fatalf("ListCameraModels: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 model, got %d", len(list))
	}

	// Insert a camera_profile referencing the model.
	height := 1.5
	profile := &storage.CameraProfile{
		CameraID:        "cam_tank_01_01",
		TankID:          "tank_01",
		DisplayName:     "Tank 01 #1 (stereo)",
		Status:          "configured",
		Position:        "overhead",
		Purpose:         []string{"vision_ai"},
		StreamProfiles:  map[string]any{},
		ClipPolicy:      map[string]any{},
		Metadata:        map[string]any{},
		ModelID:         "stereolabs_zed2i",
		MountingHeightM: &height,
	}
	if err := store.UpsertCameraProfile(ctx, profile); err != nil {
		t.Fatalf("UpsertCameraProfile: %v", err)
	}

	// FK guard — delete model with child should report count > 0.
	n, err := store.CountCameraProfilesForModel(ctx, "stereolabs_zed2i")
	if err != nil {
		t.Fatalf("CountCameraProfilesForModel: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 child instance, got %d", n)
	}

	gotProfile, err := store.GetCameraProfile(ctx, "cam_tank_01_01")
	if err != nil {
		t.Fatalf("GetCameraProfile: %v", err)
	}
	if gotProfile.ModelID != "stereolabs_zed2i" {
		t.Fatalf("expected model_id link, got %q", gotProfile.ModelID)
	}
	if gotProfile.MountingHeightM == nil || *gotProfile.MountingHeightM != 1.5 {
		t.Fatalf("mounting_height_m round-trip failed: %+v", gotProfile.MountingHeightM)
	}

	// Delete child first, then model deletes successfully.
	if err := store.DeleteCameraProfile(ctx, "cam_tank_01_01"); err != nil {
		t.Fatalf("DeleteCameraProfile: %v", err)
	}
	if err := store.DeleteCameraModel(ctx, "stereolabs_zed2i"); err != nil {
		t.Fatalf("DeleteCameraModel: %v", err)
	}
}
