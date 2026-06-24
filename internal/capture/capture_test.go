package capture

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeDummyMP4 — testdata 파일 의존 회피용 fake mp4 (capture 패키지는 mp4 디코딩 안 함).
func writeDummyMP4(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 256)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorker_Disabled(t *testing.T) {
	w := New(Config{Enabled: false}, nil)
	r, err := w.OnCycleStart(context.Background(), "cycle_x", "tank_01")
	if err != nil {
		t.Fatalf("disabled worker must not error: %v", err)
	}
	if r != nil {
		t.Fatalf("disabled worker must return nil result, got %+v", r)
	}
}

func TestWorker_FixtureMode_Copy(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "fixture.mp4")
	writeDummyMP4(t, fixture, 4096)

	tempDir := filepath.Join(tmp, "captures")
	w := New(Config{
		Enabled:         true,
		Mode:            ModeFixture,
		FixturePath:     fixture,
		DurationSeconds: 7,
		TempDir:         tempDir,
	}, nil)

	r, err := w.OnCycleStart(context.Background(), "cycle_abc", "tank_01")
	if err != nil {
		t.Fatalf("OnCycleStart: %v", err)
	}
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.CycleID != "cycle_abc" || r.TankID != "tank_01" {
		t.Fatalf("unexpected ids: %+v", r)
	}
	if r.CameraID != "fixture" {
		t.Fatalf("camera_id should be 'fixture' in fixture mode, got %q", r.CameraID)
	}
	if r.DurationS != 7 {
		t.Fatalf("duration_s expected 7, got %d", r.DurationS)
	}
	if r.MP4Path != filepath.Join(tempDir, "cycle_abc.mp4") {
		t.Fatalf("unexpected mp4 path: %s", r.MP4Path)
	}
	info, err := os.Stat(r.MP4Path)
	if err != nil {
		t.Fatalf("output mp4 not created: %v", err)
	}
	if info.Size() != 4096 {
		t.Fatalf("output size mismatch: got %d want 4096", info.Size())
	}
	if r.CapturedAt.IsZero() {
		t.Fatal("CapturedAt must be set")
	}
}

func TestWorker_FixtureMode_EmptyPath(t *testing.T) {
	w := New(Config{Enabled: true, Mode: ModeFixture, TempDir: t.TempDir()}, nil)
	_, err := w.OnCycleStart(context.Background(), "cycle_x", "tank_01")
	if err == nil {
		t.Fatal("expected error when fixture_path is empty")
	}
}

func TestWorker_FixtureMode_MissingFile(t *testing.T) {
	w := New(Config{
		Enabled:     true,
		Mode:        ModeFixture,
		FixturePath: "/nonexistent/path/fixture.mp4",
		TempDir:     t.TempDir(),
	}, nil)
	_, err := w.OnCycleStart(context.Background(), "cycle_x", "tank_01")
	if err == nil {
		t.Fatal("expected error when fixture file missing")
	}
}

func TestWorker_DefaultModeIsFixture(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "f.mp4")
	writeDummyMP4(t, fixture, 100)
	w := New(Config{Enabled: true, Mode: "", FixturePath: fixture, TempDir: tmp}, nil)
	if _, err := w.OnCycleStart(context.Background(), "c1", "tank_01"); err != nil {
		t.Fatalf("default mode should be fixture: %v", err)
	}
}

func TestWorker_UnknownMode(t *testing.T) {
	w := New(Config{Enabled: true, Mode: "unknown", TempDir: t.TempDir()}, nil)
	_, err := w.OnCycleStart(context.Background(), "c1", "tank_01")
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestWorker_RTSPMode_NilResolver(t *testing.T) {
	w := New(Config{Enabled: true, Mode: ModeRTSP, TempDir: t.TempDir()}, nil)
	_, err := w.OnCycleStart(context.Background(), "c1", "tank_01")
	if err == nil {
		t.Fatal("expected error when resolver is nil in rtsp mode")
	}
}

func TestWorker_CleanupOnce_Disabled(t *testing.T) {
	w := New(Config{Enabled: true, Mode: ModeFixture, RetentionMinutes: 0, TempDir: t.TempDir()}, nil)
	if err := w.CleanupOnce(); err != nil {
		t.Fatalf("CleanupOnce with retention=0 should be no-op: %v", err)
	}
}

func TestWorker_CleanupOnce_RemovesOldFiles(t *testing.T) {
	tmp := t.TempDir()
	old := filepath.Join(tmp, "old.mp4")
	young := filepath.Join(tmp, "young.mp4")
	writeDummyMP4(t, old, 100)
	writeDummyMP4(t, young, 100)
	// old 파일을 2시간 전 mtime 으로
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatal(err)
	}

	w := New(Config{Enabled: true, Mode: ModeFixture, RetentionMinutes: 60, TempDir: tmp}, nil)
	if err := w.CleanupOnce(); err != nil {
		t.Fatalf("CleanupOnce: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old file should be removed")
	}
	if _, err := os.Stat(young); err != nil {
		t.Fatalf("young file should remain: %v", err)
	}
}

func TestWorker_CleanupOnce_TempDirMissing(t *testing.T) {
	w := New(Config{Enabled: true, Mode: ModeFixture, RetentionMinutes: 60, TempDir: "/tmp/bluei-edge-capture-nonexistent-xyz"}, nil)
	if err := w.CleanupOnce(); err != nil {
		t.Fatalf("CleanupOnce with missing dir should be no-op: %v", err)
	}
}

func TestWorker_OnResult_CalledAfterCapture(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "f.mp4")
	writeDummyMP4(t, fixture, 100)
	w := New(Config{Enabled: true, Mode: ModeFixture, FixturePath: fixture, TempDir: tmp}, nil)

	var got *Result
	w.SetOnResult(func(_ context.Context, r *Result) {
		got = r
	})
	if _, err := w.OnCycleStart(context.Background(), "cycle_cb", "tank_99"); err != nil {
		t.Fatalf("OnCycleStart: %v", err)
	}
	if got == nil {
		t.Fatal("callback not invoked")
	}
	if got.CycleID != "cycle_cb" || got.TankID != "tank_99" || got.CameraID != "fixture" {
		t.Fatalf("callback received wrong result: %+v", got)
	}
}

func TestWorker_OnResult_NotCalledWhenDisabled(t *testing.T) {
	w := New(Config{Enabled: false}, nil)
	called := false
	w.SetOnResult(func(_ context.Context, _ *Result) { called = true })
	if _, err := w.OnCycleStart(context.Background(), "x", "y"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("callback must not be invoked when worker is disabled")
	}
}

func TestWorker_OnResult_NotCalledOnCaptureError(t *testing.T) {
	w := New(Config{Enabled: true, Mode: ModeFixture, FixturePath: "/nonexistent.mp4", TempDir: t.TempDir()}, nil)
	called := false
	w.SetOnResult(func(_ context.Context, _ *Result) { called = true })
	if _, err := w.OnCycleStart(context.Background(), "x", "y"); err == nil {
		t.Fatal("expected error for missing fixture")
	}
	if called {
		t.Fatal("callback must not be invoked on capture error")
	}
}
