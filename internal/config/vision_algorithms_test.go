package config_test

import (
	"testing"

	"bluei.kr/edge/internal/config"
)

func TestLoadVisionAlgorithmsExample(t *testing.T) {
	vc, err := config.LoadVisionAlgorithms("../../configs/vision-algorithms.example.yaml")
	if err != nil {
		t.Fatalf("LoadVisionAlgorithms: %v", err)
	}
	if vc.AlgorithmLibraryVersion != 1 {
		t.Fatalf("version = %d, want 1", vc.AlgorithmLibraryVersion)
	}
	if len(vc.Algorithms) == 0 {
		t.Fatal("expected at least one algorithm")
	}
	algo := vc.Algorithms[0]
	if algo.VisionAlgorithmID == "" || algo.Species == "" || algo.CameraPosition == "" {
		t.Fatalf("incomplete algorithm: %#v", algo)
	}
	if len(algo.Outputs) == 0 {
		t.Fatal("algorithm outputs must not be empty")
	}
	if len(vc.TankApplications) == 0 {
		t.Fatal("expected at least one tank application")
	}
	app := vc.TankApplications[0]
	if app.AppliedVisionAlgorithmID != algo.VisionAlgorithmID {
		t.Fatalf("tank application algorithm = %q, want %q", app.AppliedVisionAlgorithmID, algo.VisionAlgorithmID)
	}
}
