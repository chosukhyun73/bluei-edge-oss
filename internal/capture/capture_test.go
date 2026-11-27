package capture

import (
	"context"
	"errors"
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

func TestWorker_CleanupOnce_PreservesFeedingClips(t *testing.T) {
	tmp := t.TempDir()
	oldBaseline := filepath.Join(tmp, "clip_tank_01_cam_x_20260704T000000_000Z.mp4") // baseline
	oldFeeding := filepath.Join(tmp, "cycle_01ABC_cam_x.mp4")                        // 급이(학습)
	writeDummyMP4(t, oldBaseline, 100)
	writeDummyMP4(t, oldFeeding, 100)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	for _, f := range []string{oldBaseline, oldFeeding} {
		if err := os.Chtimes(f, twoHoursAgo, twoHoursAgo); err != nil {
			t.Fatal(err)
		}
	}

	w := New(Config{Enabled: true, Mode: ModeFixture, RetentionMinutes: 60, TempDir: tmp}, nil)
	if err := w.CleanupOnce(); err != nil {
		t.Fatalf("CleanupOnce: %v", err)
	}
	// baseline 은 60분 초과 → 삭제.
	if _, err := os.Stat(oldBaseline); !os.IsNotExist(err) {
		t.Fatal("old baseline(clip_) 은 삭제되어야 함")
	}
	// 급이(cycle_) 는 오래되어도 학습 데이터라 보존.
	if _, err := os.Stat(oldFeeding); err != nil {
		t.Fatalf("급이(cycle_) 클립은 retention 에서 보존되어야 함: %v", err)
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

// 신호게이팅 급이 캡처 — 시작 후 정지(마지막 OFF) 시점에 clip 저장 + phase=feeding onResult.
func TestWorker_CycleRecording_FixtureLifecycle(t *testing.T) {
	tmp := t.TempDir()
	fixture := filepath.Join(tmp, "f.mp4")
	writeDummyMP4(t, fixture, 2048)
	w := New(Config{Enabled: true, Mode: ModeFixture, FixturePath: fixture, TempDir: tmp}, nil)

	var got *Result
	w.SetOnResult(func(_ context.Context, r *Result) { got = r })

	if err := w.StartCycleRecording(context.Background(), "cycle_g1", "tank_01"); err != nil {
		t.Fatalf("StartCycleRecording: %v", err)
	}
	// StopCycleRecording 은 clip 완료(onResult)까지 대기하므로, 반환 후 got 이 채워져 있어야 한다.
	w.StopCycleRecording(context.Background(), "cycle_g1")
	if got == nil {
		t.Fatal("onResult not called after stop")
	}
	if got.Phase != PhaseFeeding {
		t.Fatalf("phase expected feeding, got %q", got.Phase)
	}
	if got.Reason != ReasonFeedCycleGated {
		t.Fatalf("reason expected %q, got %q", ReasonFeedCycleGated, got.Reason)
	}
	if got.CycleID != "cycle_g1" || got.CameraID != "fixture" {
		t.Fatalf("unexpected ids: %+v", got)
	}
	if _, err := os.Stat(got.MP4Path); err != nil {
		t.Fatalf("gated clip not written: %v", err)
	}
}

func TestWorker_CycleRecording_DisabledNoop(t *testing.T) {
	w := New(Config{Enabled: false}, nil)
	called := false
	w.SetOnResult(func(_ context.Context, _ *Result) { called = true })
	if err := w.StartCycleRecording(context.Background(), "x", "y"); err != nil {
		t.Fatalf("disabled start must not error: %v", err)
	}
	w.StopCycleRecording(context.Background(), "x") // 등록된 녹화 없음 — no-op, no panic
	if called {
		t.Fatal("callback must not be invoked when disabled")
	}
}

func TestWorker_CycleRecording_FixtureEmptyPath(t *testing.T) {
	w := New(Config{Enabled: true, Mode: ModeFixture, TempDir: t.TempDir()}, nil)
	if err := w.StartCycleRecording(context.Background(), "x", "y"); err == nil {
		t.Fatal("expected error when fixture_path is empty")
	}
}

// 사이클 녹화 시작 재시도 — 순단이 사이클 전체를 날리지 않아야 한다.
// (2026-08-12 11:30 tank_01: 시작 2초 만에 상단 404 → 곡선·r3 통째 손실)
func TestCaptureWithRetry(t *testing.T) {
	t.Run("첫 시도 성공 — 재시도 없음", func(t *testing.T) {
		calls, retries := 0, 0
		at, err := captureWithRetry(context.Background(), time.Second, time.Millisecond,
			func() error { calls++; return nil },
			func(int, error) { retries++ })
		if err != nil || calls != 1 || retries != 0 {
			t.Fatalf("calls=%d retries=%d err=%v", calls, retries, err)
		}
		if at.IsZero() {
			t.Error("성공인데 시작 시각이 zero")
		}
	})

	t.Run("두 번 실패 후 성공 — 성공한 시도의 시각을 돌려준다", func(t *testing.T) {
		calls, retries := 0, 0
		before := time.Now().UTC()
		at, err := captureWithRetry(context.Background(), 5*time.Second, 20*time.Millisecond,
			func() error {
				calls++
				if calls < 3 {
					return errors.New("404")
				}
				return nil
			},
			func(int, error) { retries++ })
		if err != nil || calls != 3 || retries != 2 {
			t.Fatalf("calls=%d retries=%d err=%v", calls, retries, err)
		}
		// 실패 2회 × 20ms 만큼 뒤여야 한다 — 사이클 시작 시각을 그대로 쓰면
		// 판정이 그 차이만큼 곡선을 앞당겨 읽는다.
		if at.Sub(before) < 30*time.Millisecond {
			t.Errorf("늦게 시작했는데 시각이 앞당겨짐: %v", at.Sub(before))
		}
	})

	t.Run("창 만료 — 포기하고 마지막 에러", func(t *testing.T) {
		calls := 0
		want := errors.New("계속 실패")
		at, err := captureWithRetry(context.Background(), 60*time.Millisecond, 20*time.Millisecond,
			func() error { calls++; return want }, nil)
		if !errors.Is(err, want) {
			t.Fatalf("err=%v, want %v", err, want)
		}
		if !at.IsZero() {
			t.Error("실패인데 시작 시각이 채워짐 — 곡선 없는 Result 가 적재된다")
		}
		if calls < 2 {
			t.Errorf("창 안에서 재시도하지 않음 (calls=%d)", calls)
		}
	})

	t.Run("ctx 취소 — 즉시 중단", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		at, err := captureWithRetry(ctx, time.Minute, time.Millisecond,
			func() error { calls++; return errors.New("x") }, nil)
		if err == nil || !at.IsZero() {
			t.Fatalf("취소됐는데 성공 취급: at=%v err=%v", at, err)
		}
		if calls != 1 {
			t.Errorf("취소 후에도 재시도함 (calls=%d)", calls)
		}
	})
}

// TestLaunchCleanup_Wired — retention 정리 goroutine 이 실제로 배선됐는지.
// 2026-08-18 이전에는 CleanupOnce 구현만 있고 호출부가 없어 상시 클립이
// 157,038개·273 GB 까지 누적됐다. 배선이 끊기면 같은 사고가 반복된다.
func TestLaunchCleanup_Wired(t *testing.T) {
	tmp := t.TempDir()
	old := filepath.Join(tmp, "clip_tank_01_cam_x_20260704T000000_000Z.mp4")
	writeDummyMP4(t, old, 100)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := New(Config{Enabled: true, Mode: ModeFixture, RetentionMinutes: 60, TempDir: tmp}, nil)
	w.launchCleanup(ctx) // 기동 직후 1회는 ticker 를 기다리지 않는다

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(old); os.IsNotExist(err) {
			return // 정리됨
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("launchCleanup 이 기동 직후 CleanupOnce 를 호출하지 않았다")
}

// TestLaunchCleanup_PreservedDirUntouched — 라벨 붙은 상시 클립은 preserved/ 하위로
// 옮겨 보호한다. cleanupMP4Dir 은 디렉터리를 건너뛰므로 retention 이 지나도 남는다.
func TestLaunchCleanup_PreservedDirUntouched(t *testing.T) {
	tmp := t.TempDir()
	pdir := filepath.Join(tmp, "preserved")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	labeled := filepath.Join(pdir, "clip_tank_01_cam_x_20260703T062106_453Z.mp4")
	writeDummyMP4(t, labeled, 100)
	old := time.Now().Add(-90 * 24 * time.Hour) // 90일 전 — 어떤 retention 에도 걸릴 나이
	if err := os.Chtimes(labeled, old, old); err != nil {
		t.Fatal(err)
	}

	w := New(Config{Enabled: true, Mode: ModeFixture, RetentionMinutes: 60, TempDir: tmp}, nil)
	if err := w.CleanupOnce(); err != nil {
		t.Fatalf("CleanupOnce: %v", err)
	}
	if _, err := os.Stat(labeled); err != nil {
		t.Fatalf("preserved/ 하위 라벨 클립은 보존되어야 한다: %v", err)
	}
}
