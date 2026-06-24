// Package capture — feed cycle 시작 시점 7초 영상 캡처 워커 (G-2).
//
// G-3 pipeline 이 결과 mp4 를 LRCN /v1/vision/feeding-score 로 보내고
// 응답을 VisionObservation 으로 적재한다.
//
// 본선 D-6 환경 (RTSP 실기 없음): mode=fixture — 미리 준비된 mp4 를 복사.
// 강릉 D-18 이후 실기 환경: mode=rtsp — gst-launch-1.0 + rtspsrc 로 캡처.
package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultDurationSeconds  = 7
	DefaultTempDir          = "/tmp/bluei-edge/captures"
	DefaultRetentionMinutes = 60
	ModeFixture             = "fixture"
	ModeRTSP                = "rtsp"

	// PhaseFeeding — cycle 활성 중에 캡처된 clip (LRCN 사료 반응 학습 자료).
	PhaseFeeding = "feeding"
	// PhaseBaseline — cycle 외 시간에 캡처된 clip (안정성 학습 자료).
	PhaseBaseline = "baseline"
)

// Config — capture worker 설정 (configs/edge.example.yaml capture: 섹션).
type Config struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Mode             string `yaml:"mode" json:"mode"`                           // fixture | rtsp
	FixturePath      string `yaml:"fixture_path" json:"fixture_path"`           // mode=fixture
	DurationSeconds  int    `yaml:"duration_seconds" json:"duration_seconds"`   // default 7
	TempDir          string `yaml:"temp_dir" json:"temp_dir"`                   // default /tmp/bluei-edge/captures
	RetentionMinutes int    `yaml:"retention_minutes" json:"retention_minutes"` // default 60. 0 = never cleanup

	// R6.1 — 상시 캡처 모드.
	// Continuous=true 면 cycle hook 과 *독립적으로* 모든 ContinuousTanks 의 카메라에서
	// DurationSeconds 간격으로 연속 mp4 캡처. cycle 진행 중인 clip → phase=feeding,
	// 그 외 → phase=baseline 으로 결과 분류.
	Continuous            bool     `yaml:"continuous" json:"continuous"`
	ContinuousTanks       []string `yaml:"continuous_tanks" json:"continuous_tanks"`
	ContinuousModeSpacing int      `yaml:"continuous_mode_spacing_seconds" json:"continuous_mode_spacing_seconds"` // 0=DurationSeconds 와 동일 (연속)

	// R17 — 디스크 % 임계 강제 cleanup.
	// MaxDiskPercent 초과 시 CleanupOnce 가 *RetentionMinutes 무시* 하고 *오래된 mp4 부터*
	// 디스크 사용률이 임계 아래로 떨어질 때까지 삭제. 0 또는 음수 = 비활성 (RetentionMinutes 만 적용).
	// 강릉 D-18 외장 하드 운영 시 디스크 가득 차서 캡처 중단되는 사고 방지.
	MaxDiskPercent float64 `yaml:"max_disk_percent" json:"max_disk_percent"` // 0~100, default 0=비활성
}

// CameraRef — resolver 가 반환하는 선택된 카메라.
type CameraRef struct {
	CameraID string
	RTSPURL  string // mode=rtsp 일 때만 의미.
}

// CameraResolver — tank → 측면(feeding inference) 카메라 + RTSP URL.
// 우선순위: ViewAngle=="side" → Purpose ∋ "feeding_inference" → 첫 카메라.
type CameraResolver interface {
	ResolveForFeedingInference(ctx context.Context, tankID string) (CameraRef, error)
}

// Result — capture 완료 산출 (G-3 pipeline 입력).
type Result struct {
	// CycleID — cycle hook 에서 캡처된 clip 만. 상시(continuous) 캡처는 공란.
	CycleID string `json:"cycle_id"`
	// ClipID — R6.1 상시 캡처 ULID. cycle 캡처에서는 "cycle_<cycle_id>" 형식으로 채움.
	ClipID     string    `json:"clip_id"`
	TankID     string    `json:"tank_id"`
	CameraID   string    `json:"camera_id"`
	MP4Path    string    `json:"mp4_path"`
	DurationS  int       `json:"duration_s"`
	CapturedAt time.Time `json:"captured_at"`
	// Phase — feeding (cycle 활성 중) | baseline (cycle 외).
	// 상시 캡처는 호출자(continuous loop) 가 직접 결정. cycle hook 캡처는 항상 feeding.
	Phase string `json:"phase"`
}

// Worker — capture 워커.
type Worker struct {
	cfg      Config
	resolver CameraResolver
	onResult func(context.Context, *Result) // G-3 — pipeline 트리거. nil 허용.
	log      *slog.Logger
}

// SetOnResult — G-3. capture 완료 후 호출될 callback 등록. nil 허용.
// callback 은 capture 의 background goroutine 안에서 sync 호출되므로 LRCN 1~2s 무방.
func (w *Worker) SetOnResult(h func(context.Context, *Result)) {
	w.onResult = h
}

// New — Worker 생성. resolver 는 mode=fixture 일 때 nil 허용 (테스트 친화).
func New(cfg Config, resolver CameraResolver) *Worker {
	if cfg.DurationSeconds == 0 {
		cfg.DurationSeconds = DefaultDurationSeconds
	}
	if cfg.TempDir == "" {
		cfg.TempDir = DefaultTempDir
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeFixture
	}
	return &Worker{cfg: cfg, resolver: resolver, log: slog.With("service", "capture")}
}

// Enabled — cycle hook 호출 가드.
func (w *Worker) Enabled() bool { return w.cfg.Enabled }

// OnCycleStart — feed cycle 시작 hook. 7초 mp4 생성 + Result 반환.
// 에러 시에도 cycle 자체는 진행 (capture 실패 ≠ cycle 실패). 호출자가 결정.
func (w *Worker) OnCycleStart(ctx context.Context, cycleID, tankID string) (*Result, error) {
	if !w.cfg.Enabled {
		return nil, nil
	}
	if err := os.MkdirAll(w.cfg.TempDir, 0o755); err != nil {
		return nil, fmt.Errorf("capture temp dir: %w", err)
	}
	mp4Path := filepath.Join(w.cfg.TempDir, cycleID+".mp4")
	var cameraID string

	switch strings.ToLower(w.cfg.Mode) {
	case ModeFixture:
		if w.cfg.FixturePath == "" {
			return nil, fmt.Errorf("capture mode=fixture but fixture_path is empty")
		}
		if err := copyFile(w.cfg.FixturePath, mp4Path); err != nil {
			return nil, err
		}
		cameraID = "fixture"
	case ModeRTSP:
		if w.resolver == nil {
			return nil, fmt.Errorf("capture mode=rtsp but resolver is nil")
		}
		ref, err := w.resolver.ResolveForFeedingInference(ctx, tankID)
		if err != nil {
			return nil, fmt.Errorf("camera resolve for tank %s: %w", tankID, err)
		}
		if err := captureRTSP(ctx, ref.RTSPURL, mp4Path, w.cfg.DurationSeconds); err != nil {
			return nil, err
		}
		cameraID = ref.CameraID
	default:
		return nil, fmt.Errorf("capture mode %q unsupported", w.cfg.Mode)
	}

	w.log.Info("capture completed",
		"cycle_id", cycleID, "tank_id", tankID, "camera_id", cameraID,
		"mp4_path", mp4Path, "mode", w.cfg.Mode)

	result := &Result{
		CycleID: cycleID,
		// cycleID 가 이미 "cycle_<ULID>" prefix 를 포함 (common.NewID("cycle")). 그대로 사용.
		ClipID:     cycleID,
		TankID:     tankID,
		CameraID:   cameraID,
		MP4Path:    mp4Path,
		DurationS:  w.cfg.DurationSeconds,
		CapturedAt: time.Now().UTC(),
		Phase:      PhaseFeeding, // cycle hook 캡처는 항상 feeding
	}
	if w.onResult != nil {
		w.onResult(ctx, result)
	}
	return result, nil
}

// CaptureContinuous — R6.1 상시 캡처 1회. tank 의 측면 카메라에서 DurationSeconds mp4
// 한 개 생성. cycle 진행 중인지 여부는 phaseDecider 콜백이 결정 (R6.2 에서 events
// active feed_cycle 조회 후 적재).
//
// 결과 mp4 명: "clip_<ULID>.mp4". cycle hook 의 "cycle_<id>.mp4" 와 충돌 없음.
// fixture 모드에서는 같은 fixture 영상 반복 복사라 *원칙적으로 권장 X*, 단 dev 검증 위해 허용.
func (w *Worker) CaptureContinuous(
	ctx context.Context,
	tankID string,
	resolveCamera func(context.Context, string) (CameraRef, error),
	decidePhase func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string),
) (*Result, error) {
	if !w.cfg.Enabled || !w.cfg.Continuous {
		return nil, nil
	}
	if err := os.MkdirAll(w.cfg.TempDir, 0o755); err != nil {
		return nil, fmt.Errorf("capture temp dir: %w", err)
	}

	// ULID-like timestamp ID. internal/common.NewID 가 events 다른 곳에서도 ulid 형식이라
	// 일관성 차원에서 거기서 빌려옴 — 단 import cycle 피해서 timestamp 기반 단순 ID.
	startTS := time.Now().UTC()
	clipID := fmt.Sprintf("clip_%s_%s", tankID, startTS.Format("20060102T150405.000Z"))
	clipID = strings.ReplaceAll(clipID, ".", "_") // 파일명 안전
	mp4Path := filepath.Join(w.cfg.TempDir, clipID+".mp4")
	var cameraID string

	switch strings.ToLower(w.cfg.Mode) {
	case ModeFixture:
		if w.cfg.FixturePath == "" {
			return nil, fmt.Errorf("capture mode=fixture but fixture_path is empty")
		}
		if err := copyFile(w.cfg.FixturePath, mp4Path); err != nil {
			return nil, err
		}
		cameraID = "fixture"
	case ModeRTSP:
		if resolveCamera == nil {
			return nil, fmt.Errorf("capture continuous mode=rtsp but resolveCamera is nil")
		}
		ref, err := resolveCamera(ctx, tankID)
		if err != nil {
			return nil, fmt.Errorf("camera resolve for tank %s: %w", tankID, err)
		}
		if err := captureRTSP(ctx, ref.RTSPURL, mp4Path, w.cfg.DurationSeconds); err != nil {
			return nil, err
		}
		cameraID = ref.CameraID
	default:
		return nil, fmt.Errorf("capture mode %q unsupported", w.cfg.Mode)
	}

	phase := PhaseBaseline
	cycleID := ""
	if decidePhase != nil {
		phase, cycleID = decidePhase(ctx, tankID, startTS)
		if phase == "" {
			phase = PhaseBaseline
		}
	}

	w.log.Info("continuous capture completed",
		"clip_id", clipID, "tank_id", tankID, "camera_id", cameraID,
		"mp4_path", mp4Path, "phase", phase, "cycle_id", cycleID, "mode", w.cfg.Mode)

	result := &Result{
		ClipID:     clipID,
		CycleID:    cycleID,
		TankID:     tankID,
		CameraID:   cameraID,
		MP4Path:    mp4Path,
		DurationS:  w.cfg.DurationSeconds,
		CapturedAt: startTS,
		Phase:      phase,
	}
	if w.onResult != nil {
		w.onResult(ctx, result)
	}
	return result, nil
}

// RunContinuous — R6.1 상시 캡처 loop. 모든 ContinuousTanks 에 대해 병렬 goroutine.
// ctx cancel 시 graceful 종료.
//
// 각 tank goroutine 패턴: capture → spacing 만큼 대기 → 반복.
// spacing=0 이면 DurationSeconds 그대로 (back-to-back). 작은 spacing 으로 CPU 부담 조절 가능.
//
// 호출자가 resolveCamera 와 decidePhase 콜백을 제공. capture 가 runtime/storage 에 import
// cycle 안 만들기 위한 의존성 역전.
func (w *Worker) RunContinuous(
	ctx context.Context,
	resolveCamera func(context.Context, string) (CameraRef, error),
	decidePhase func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string),
) error {
	if !w.cfg.Enabled || !w.cfg.Continuous {
		return nil
	}
	if len(w.cfg.ContinuousTanks) == 0 {
		w.log.Warn("continuous mode enabled but continuous_tanks is empty — nothing to capture")
		return nil
	}
	spacing := time.Duration(w.cfg.ContinuousModeSpacing) * time.Second
	if spacing <= 0 {
		spacing = time.Duration(w.cfg.DurationSeconds) * time.Second
	}
	w.log.Info("continuous capture loop starting",
		"tanks", w.cfg.ContinuousTanks, "duration_s", w.cfg.DurationSeconds, "spacing", spacing)

	for _, tankID := range w.cfg.ContinuousTanks {
		t := tankID // capture
		go func() {
			ticker := time.NewTicker(spacing)
			defer ticker.Stop()
			// 첫 capture 는 즉시
			if _, err := w.CaptureContinuous(ctx, t, resolveCamera, decidePhase); err != nil {
				w.log.Warn("continuous capture failed", "tank_id", t, "err", err)
			}
			for {
				select {
				case <-ctx.Done():
					w.log.Info("continuous capture loop stopped", "tank_id", t)
					return
				case <-ticker.C:
					if _, err := w.CaptureContinuous(ctx, t, resolveCamera, decidePhase); err != nil {
						w.log.Warn("continuous capture failed", "tank_id", t, "err", err)
					}
				}
			}
		}()
	}
	return nil
}

// CleanupOnce — TempDir 안에서 RetentionMinutes 초과 mp4 삭제. RetentionMinutes=0 이면 no-op.
// 정기 호출은 G-3 pipeline 의 cleanup goroutine 또는 별도 worker 가 담당.
//
// R8.4: excluded/<reason>/ 하위도 cleanup 대상 (판단 불가 격리된 영상).
// 운영자가 격리한 영상은 학습에 안 쓰이므로 retention 동일하게 적용 (별도 더 짧은 정책은 향후).
//
// R17: MaxDiskPercent > 0 이면 정상 retention 후에도 디스크 임계 검사 → 초과 시 오래된 mp4
// 강제 삭제 (디스크 가득 차서 캡처 중단 사고 방지).
func (w *Worker) CleanupOnce() error {
	if w.cfg.RetentionMinutes > 0 {
		cutoff := time.Now().Add(-time.Duration(w.cfg.RetentionMinutes) * time.Minute)
		if err := cleanupMP4Dir(w.cfg.TempDir, cutoff); err != nil {
			return err
		}
		// excluded/ 하위 recursive
		excludedRoot := filepath.Join(w.cfg.TempDir, "excluded")
		if entries, err := os.ReadDir(excludedRoot); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				_ = cleanupMP4Dir(filepath.Join(excludedRoot, e.Name()), cutoff)
			}
		}
	}

	// R17 — 디스크 임계 강제 cleanup
	if w.cfg.MaxDiskPercent > 0 && w.cfg.MaxDiskPercent < 100 {
		if err := w.cleanupByDiskPercent(); err != nil {
			w.log.Warn("max_disk_percent cleanup failed", "err", err)
		}
	}
	return nil
}

// cleanupByDiskPercent — 디스크 사용률이 MaxDiskPercent 초과면 가장 오래된 mp4
// (captures + excluded 모두) 부터 삭제, 임계 아래로 떨어질 때까지.
func (w *Worker) cleanupByDiskPercent() error {
	used, err := diskUsedPercent(w.cfg.TempDir)
	if err != nil {
		return err
	}
	if used <= w.cfg.MaxDiskPercent {
		return nil
	}
	// 모든 mp4 (captures + excluded/<reason>) 를 mtime 오름차순으로 수집.
	type file struct {
		path  string
		mtime time.Time
	}
	var all []file
	collect := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".mp4" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			all = append(all, file{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	collect(w.cfg.TempDir)
	excludedRoot := filepath.Join(w.cfg.TempDir, "excluded")
	if subs, err := os.ReadDir(excludedRoot); err == nil {
		for _, s := range subs {
			if s.IsDir() {
				collect(filepath.Join(excludedRoot, s.Name()))
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mtime.Before(all[j].mtime) })

	// 임계 아래로 떨어질 때까지 오래된 것 부터 삭제 (max 100건 안전 limit).
	deleted := 0
	for _, f := range all {
		if deleted >= 100 {
			break
		}
		if err := os.Remove(f.path); err == nil {
			deleted++
			w.log.Info("max_disk_percent cleanup deleted", "path", f.path, "mtime", f.mtime)
		}
		// 매 10건 마다 재측정
		if deleted%10 == 0 {
			if u, err := diskUsedPercent(w.cfg.TempDir); err == nil && u <= w.cfg.MaxDiskPercent {
				break
			}
		}
	}
	if deleted > 0 {
		w.log.Warn("max_disk_percent triggered cleanup",
			"threshold_percent", w.cfg.MaxDiskPercent,
			"used_before_percent", used,
			"deleted_files", deleted)
	}
	return nil
}

// diskUsedPercent — 디렉토리가 속한 디스크의 사용률 (%). statfs.
func diskUsedPercent(dir string) (float64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return 0, err
	}
	total := int64(fs.Blocks) * int64(fs.Bsize)
	free := int64(fs.Bavail) * int64(fs.Bsize)
	if total <= 0 {
		return 0, nil
	}
	return float64(total-free) / float64(total) * 100, nil
}

func cleanupMP4Dir(dir string, cutoff time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".mp4" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("fixture open: %w", err)
	}
	defer srcF.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("capture create: %w", err)
	}
	defer dstF.Close()
	if _, err := io.Copy(dstF, srcF); err != nil {
		return fmt.Errorf("fixture copy: %w", err)
	}
	return nil
}

// captureRTSP — gst-launch-1.0 으로 RTSP → mp4 N초 캡처. snapshotJPEG (internal/api/cameras.go:580) 와 같은 패턴.
// SIGTERM 으로 graceful 종료해야 mp4mux 가 깨끗하게 close (SIGKILL 시 mp4 손상).
func captureRTSP(parent context.Context, rtspURL, outPath string, durationSec int) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return fmt.Errorf("gst-launch-1.0 is not installed")
	}
	if rtspURL == "" {
		return fmt.Errorf("rtsp_url is required")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(durationSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q", "-e",
		"rtspsrc", "location="+rtspURL, "latency=800", "protocols=tcp",
		"!", "rtph264depay", "!", "h264parse",
		"!", "mp4mux",
		"!", "filesink", "location="+outPath)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		// SIGTERM 으로 정상 종료된 케이스 — mp4 파일이 생성되었으면 성공으로 간주
		if info, statErr := os.Stat(outPath); statErr == nil && info.Size() > 0 {
			return nil
		}
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("rtsp capture failed: %s", msg)
	}
	return nil
}
