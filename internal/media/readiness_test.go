package media

import "testing"

func TestRuntimeReadinessUsesInjectedProbe(t *testing.T) {
	readiness := CheckRuntime(RuntimeProbeFunc(func(name string) bool {
		return name == "gst-launch-1.0" || name == "rtspsrc"
	}))

	if !readiness.Tools["gst-launch-1.0"] {
		t.Fatal("expected gst-launch-1.0 ready")
	}
	if !readiness.Plugins["rtspsrc"] {
		t.Fatal("expected rtspsrc ready")
	}
	if readiness.Plugins["avdec_h264"] {
		t.Fatal("expected avdec_h264 to be unavailable in this probe")
	}
}
