package media

import "testing"

func TestStreamLimitsValidate(t *testing.T) {
	limits := StreamLimits{MaxWorkers: 8, MaxViewersPerWorker: 4}
	if err := limits.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestStreamLimitsRejectsInvalidValues(t *testing.T) {
	cases := []StreamLimits{
		{MaxWorkers: -1, MaxViewersPerWorker: 1},
		{MaxWorkers: 1, MaxViewersPerWorker: -1},
	}
	for _, tc := range cases {
		if err := tc.Validate(); err == nil {
			t.Fatalf("expected invalid limits error for %#v", tc)
		}
	}
}

func TestStreamLimitsAllowChecks(t *testing.T) {
	limits := StreamLimits{MaxWorkers: 2, MaxViewersPerWorker: 3}
	if !limits.AllowWorker(1) {
		t.Fatal("expected worker allowed")
	}
	if limits.AllowWorker(2) {
		t.Fatal("expected worker denied at max")
	}
	if !limits.AllowViewer(2) {
		t.Fatal("expected viewer allowed")
	}
	if limits.AllowViewer(3) {
		t.Fatal("expected viewer denied at max")
	}
}
