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
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultDurationSeconds  = 7
	DefaultTempDir          = "/tmp/bluei-edge/captures"
	DefaultRetentionMinutes = 60
	ModeFixture             = "fixture"
	ModeRTSP                = "rtsp"

	// cleanupInterval — retention 정리 주기. 상시 캡처가 하루 6 GB 쌓이므로
	// 시간 단위면 충분하고, 25만 파일 스캔이 캡처 I/O 와 겹치는 빈도도 낮다.
	cleanupInterval = 1 * time.Hour

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

	// 관찰 꼬리 (2026-07-06) — 사이클 종료 후 게이팅 녹화 유지 시간(초). 0 = 즉시 종료.
	ObservationTailSec int `yaml:"observation_tail_sec" json:"observation_tail_sec"`
}

// CameraRef — resolver 가 반환하는 선택된 카메라.
type CameraRef struct {
	CameraID string
	RTSPURL  string // mode=rtsp 일 때만 의미.
	// 크롭 비율(0~1). 크롭공유 카메라(한 화면에 2수조)를 수조별로 분리 캡처할 때 각 변에서
	// 잘라낼 비율. 0=크롭 없음(무재인코딩 passthrough). >0 이면 decode→videocrop→x264 재인코딩.
	CropLeft  float64
	CropRight float64
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
	// Reason — media.clip.stored 의 reason. 공란이면 호출부가 "continuous_capture" 로 기본 처리.
	// 신호게이팅 급이 clip 은 ReasonFeedCycleGated.
	Reason string `json:"reason,omitempty"`
}

// Worker — capture 워커.
type Worker struct {
	cfg      Config
	resolver CameraResolver
	onResult func(context.Context, *Result) // G-3 — pipeline 트리거. nil 허용.
	// onNightClip — dark-sleep 폐기 대상(소등/IR) baseline 클립 콜백. 설정 시
	// 클립 파일 삭제 책임까지 콜백이 인수한다 (야간 활동성 측정 후 삭제). nil 허용.
	onNightClip func(context.Context, *Result)
	log         *slog.Logger

	// 런타임 제어 (대시보드 "수집 시작/중지" 버튼). config.Continuous 와 별개로
	// 운영자가 켜고 끌 수 있다. ConfigureRuntime 으로 base ctx + 콜백을 등록한 뒤
	// StartContinuous/StopContinuous 로 상시 캡처 루프를 토글한다.
	mu            sync.Mutex
	cancel        context.CancelFunc
	running       bool
	baseCtx       context.Context
	resolveCamera func(context.Context, string) ([]CameraRef, error)
	decidePhase   func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string)

	// activeTank — 입식 여부 게이트(nil = 전체 캡처). 상시 캡처 루프가 매 tick 마다 호출해
	// false 면 그 tank 캡처를 skip 한다. 빈 수조 clip 유입 방지 (매 tick 재평가 → 동적).
	activeTank func(context.Context, string) bool

	// 신호게이팅 급이 녹화(StartCycleRecording/StopCycleRecording). cycleID → 진행중 녹화.
	recMu      sync.Mutex
	recordings map[string]*cycleRecording

	// 사이클 녹화 경계 훅 — 녹화와 같은 창을 쓰는 소비자(라이브 요동 세션)용. nil 허용.
	onCycleRecStart func(cycleID, tankID string)
	onCycleRecStop  func(cycleID, tankID string)

	// 실조도 야간 휴면 (2026-07-08 운영자 지시: 광주기 시계가 아니라 진짜 불이
	// 꺼지면 휴면, 켜지면 재개). tankID → 상태. lightMu 로 보호.
	lightMu sync.Mutex
	light   map[string]*tankLightState
}

// tankLightState — 수조별 조도 게이트 상태 (연속 캡처 전용, 사이클 녹화 무관).
type tankLightState struct {
	darkStreak int       // 연속 어두움 관측 수
	sleeping   bool      // 야간 휴면 중
	lastProbe  time.Time // 휴면 중 마지막 프로브 캡처 시각
}

// 조도 게이트 상수 — floor still 게이트(40)와 같은 계열. 히스테리시스로 깜빡임 방지.
const (
	captureDarkLuma   = 35.0            // 미만이면 소등 관측
	captureWakeLuma   = 50.0            // 이상이면 점등 관측 (복귀)
	captureDarkStreak = 2               // 연속 N회 어두우면 휴면 진입
	captureProbeEvery = 5 * time.Minute // 휴면 중 재점등 확인 주기
)

// cycleRecording — 한 사이클의 신호게이팅 녹화(수조 전 카메라). cancel 로 종료(SIGINT clean EOS),
// wg 로 모든 카메라 clip 의 onResult 완료를 대기.
type cycleRecording struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	tankID string
}

// SetActiveTankFilter — 상시 캡처 대상 수조 게이트 등록. nil 이면 ContinuousTanks 전체 캡처.
// main 부팅에서 fish_count>0(입식) 수조만 캡처하도록 주입.
func (w *Worker) SetActiveTankFilter(fn func(context.Context, string) bool) {
	w.mu.Lock()
	w.activeTank = fn
	w.mu.Unlock()
}

// SetOnResult — G-3. capture 완료 후 호출될 callback 등록. nil 허용.
// callback 은 capture 의 background goroutine 안에서 sync 호출되므로 LRCN 1~2s 무방.
func (w *Worker) SetOnResult(h func(context.Context, *Result)) {
	w.onResult = h
}

// SetOnNightClip — dark-sleep 폐기 대상(소등/IR) baseline 클립 콜백 등록. 설정 시
// mp4/jpg 삭제 책임이 콜백으로 넘어간다 (야간 활동성 측정 후 삭제). nil 허용.
func (w *Worker) SetOnNightClip(h func(context.Context, *Result)) {
	w.onNightClip = h
}

// SetCycleRecordingHooks — 신호게이팅 녹화 시작/종료 훅 등록 (축1 1a 라이브 요동 세션,
// 축3 3a 바닥 스틸). start 는 녹화가 실제 시작됐을 때만, stop 은 녹화 종료 직전 호출된다.
// 둘 다 사이클 진행 경로에서 sync 호출되므로 블로킹 작업은 훅 안에서 goroutine 으로. nil 허용.
func (w *Worker) SetCycleRecordingHooks(start, stop func(cycleID, tankID string)) {
	w.onCycleRecStart = start
	w.onCycleRecStop = stop
}

// New — Worker 생성. resolver 는 mode=fixture 일 때 nil 허용 (테스트 친화).
func New(cfg Config, resolver CameraResolver) *Worker {
	if cfg.DurationSeconds == 0 {
		cfg.DurationSeconds = DefaultDurationSeconds
	}
	if cfg.TempDir == "" {
		cfg.TempDir = DefaultTempDir
	}
	// 절대경로로 정규화 — 저장되는 clip URI 가 절대경로여야 재생 핸들러(IsAbs 검증)를
	// 통과하고, config 가 상대경로(./var/...)여도 안정적으로 동작한다.
	if abs, err := filepath.Abs(cfg.TempDir); err == nil {
		cfg.TempDir = abs
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeFixture
	}
	return &Worker{cfg: cfg, resolver: resolver, log: slog.With("service", "capture")}
}

// Enabled — cycle hook 호출 가드.
func (w *Worker) Enabled() bool { return w.cfg.Enabled }

// ObservationTailSec — 사이클 종료 후 녹화 유지 시간(초). 0 = 꼬리 없음.
func (w *Worker) ObservationTailSec() int { return w.cfg.ObservationTailSec }

// OnCycleStart — feed cycle 시작 hook (고정 7초 단일 카메라). Result 반환.
// 에러 시에도 cycle 자체는 진행 (capture 실패 ≠ cycle 실패). 호출자가 결정.
//
// ⚠ 급이 사이클 캡처는 신호게이팅(StartCycleRecording/StopCycleRecording)으로 대체됨
// (급이기 첫 ON~마지막 OFF 전 구간 1개 긴 clip, 수조 전 카메라). 이 메서드는 고정길이
// 단발 캡처가 필요한 경우를 위해 유지.
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
		if err := captureRTSP(ctx, ref.RTSPURL, mp4Path, w.cfg.DurationSeconds, ref.CropLeft, ref.CropRight); err != nil {
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

// maxCycleRecordingSeconds — 신호게이팅 녹화 백스톱. 사이클이 비정상적으로 finalize 되지
// 않아 StopCycleRecording 가 호출되지 않아도 이 시간이면 녹화가 자동 종료(런어웨이·거대파일 방지).
const maxCycleRecordingSeconds = 20 * 60

// ReasonFeedCycleGated — 신호게이팅 급이 clip 의 media.clip.stored reason (연속캡처와 구분).
const ReasonFeedCycleGated = "feed_cycle_gated"

// gatedOnResultTimeout — 게이팅 clip onResult(적재 + LRCN 스코어) 백스톱.
// inference.lrcn.timeout_sec(60) 보다 커야 한다: 카메라별 goroutine 이 동시에 Score 를
// 호출하지만 LRCN 서버는 직렬 처리라 두 번째 clip 은 첫 clip 처리 대기가 요청 시간에
// 포함된다. 8s 였을 때 2clip 수조의 수중캠 스코어가 항상 deadline 초과 (2026-07-05 실측).
const gatedOnResultTimeout = 90 * time.Second

// gatedRetryWindow / gatedRetryDelay — 사이클 녹화 시작 실패 시 재시도 창.
//
// 첫 펄스는 사이클 시작 후 ~90초에 오므로(펄스 간격 ~70초) 그 전에 붙으면 판정에
// 필요한 구간을 잃지 않는다. 창을 더 늘리면 첫 펄스 반응을 놓친 클립이 남아
// "곡선은 있는데 앞이 비어 있는" 상태가 되므로 60초에서 끊는다.
//
// ⚠ 재시도로 늦게 붙으면 Result.CapturedAt 을 **실제 시작 시각**으로 기록해야 한다.
// 판정은 (펄스시각 − CapturedAt)으로 곡선 인덱스를 구하므로, 사이클 시작으로 두면
// 늦은 만큼 전 구간이 밀려 오판이 된다.
const (
	gatedRetryWindow = 60 * time.Second
	gatedRetryDelay  = 3 * time.Second
)

// captureWithRetry — 사이클 녹화 시작 실패 시 window 안에서 delay 간격으로 재시도한다.
//
// 시작 실패는 그 사이클의 곡선·r3 를 통째로 잃는다 (2026-08-12 11:30 tank_01 실측:
// 시작 2초 만에 상단 404 → 상단 곡선 없음 + "no top-view feeding clip" 으로 r3 skip).
// 상시 캡처는 30초마다 재시도해 곧 회복되지만 사이클 녹화는 한 번뿐이라 그 순간의
// 순단이 사이클 전체를 날린다.
//
// 반환하는 시각은 **성공한 시도가 시작된 시각**이다. 판정은 (펄스시각 − CapturedAt)으로
// 곡선 인덱스를 구하므로, 늦게 붙었는데 사이클 시작 시각을 쓰면 전 구간이 밀려 오판이 된다.
// 실패 시 zero time 과 마지막 에러를 돌려준다.
func captureWithRetry(ctx context.Context, window, delay time.Duration,
	capture func() error, onRetry func(attempt int, err error)) (time.Time, error) {
	deadline := time.Now().Add(window)
	var err error
	for attempt := 1; ; attempt++ {
		startedAt := time.Now().UTC()
		if err = capture(); err == nil {
			return startedAt, nil
		}
		// ctx 종료(사이클 정지·백스톱)면 재시도가 무의미하고, 창을 넘겼으면 이미
		// 첫 펄스 구간을 잃어 늦은 클립이 오히려 해가 된다.
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return time.Time{}, err
		}
		if onRetry != nil {
			onRetry(attempt, err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
	}
}

// StartCycleRecording — 신호게이팅 급이 캡처 시작(비블로킹). 급이기 첫 ON(사이클 시작)에 호출.
// 수조에 연결된 모든 카메라(profile tank_id)에서 각각 open-ended 녹화를 띄우고,
// StopCycleRecording(마지막 OFF) 시 종료하며 phase=feeding clip 을 onResult 로 보낸다.
// D-3: ON~OFF 전 구간을 카메라별 1개 긴 clip 으로 저장(LRCN 7초 분할은 학습 시).
func (w *Worker) StartCycleRecording(ctx context.Context, cycleID, tankID string) error {
	if !w.cfg.Enabled {
		return nil
	}
	if err := os.MkdirAll(w.cfg.TempDir, 0o755); err != nil {
		return fmt.Errorf("capture temp dir: %w", err)
	}

	type target struct {
		cameraID            string
		rtspURL             string
		cropLeft, cropRight float64
	}
	var targets []target
	switch strings.ToLower(w.cfg.Mode) {
	case ModeFixture:
		if w.cfg.FixturePath == "" {
			return fmt.Errorf("capture mode=fixture but fixture_path is empty")
		}
		if _, err := os.Stat(w.cfg.FixturePath); err != nil {
			return fmt.Errorf("fixture not readable: %w", err)
		}
		targets = []target{{cameraID: "fixture"}}
	case ModeRTSP:
		resolve := w.resolveCamera
		if resolve == nil {
			return fmt.Errorf("capture mode=rtsp but resolveCamera not configured")
		}
		refs, err := resolve(ctx, tankID)
		if err != nil {
			return fmt.Errorf("camera resolve for tank %s: %w", tankID, err)
		}
		for _, r := range refs {
			targets = append(targets, target{cameraID: r.CameraID, rtspURL: r.RTSPURL, cropLeft: r.CropLeft, cropRight: r.CropRight})
		}
		if len(targets) == 0 {
			return fmt.Errorf("no camera for tank %s", tankID)
		}
	default:
		return fmt.Errorf("capture mode %q unsupported", w.cfg.Mode)
	}

	// 녹화 ctx — 앱 baseCtx(있으면) 파생 → 종료 시 자동 취소 + 백스톱 deadline.
	base := w.baseCtx
	if base == nil {
		base = context.Background()
	}
	recCtx, cancel := context.WithTimeout(base, maxCycleRecordingSeconds*time.Second)
	rec := &cycleRecording{cancel: cancel, tankID: tankID}
	startedAt := time.Now().UTC()
	mode := strings.ToLower(w.cfg.Mode)

	for _, tg := range targets {
		rec.wg.Add(1)
		go func(tg target) {
			defer rec.wg.Done()
			safeCam := strings.ReplaceAll(tg.cameraID, "/", "_")
			clipID := fmt.Sprintf("%s_%s", cycleID, safeCam)
			mp4Path := filepath.Join(w.cfg.TempDir, clipID+".mp4")

			var capErr error
			// 실제 녹화가 시작된 시각. 재시도로 늦게 붙으면 사이클 시작과 달라지며,
			// 판정은 (펄스시각 − 이 값)으로 곡선 인덱스를 구하므로 반드시 실측이어야 한다.
			actualStart := startedAt
			if mode == ModeFixture {
				<-recCtx.Done() // 정지 신호(또는 백스톱)까지 대기 후 fixture 복사
				capErr = copyFile(w.cfg.FixturePath, mp4Path)
			} else {
				actualStart, capErr = captureWithRetry(recCtx, gatedRetryWindow, gatedRetryDelay,
					func() error {
						return captureRTSPUntil(recCtx, tg.rtspURL, mp4Path, tg.cropLeft, tg.cropRight)
					},
					func(attempt int, err error) {
						w.log.Warn("gated capture 재시도 — 시작 실패",
							"cycle_id", cycleID, "tank_id", tankID, "camera_id", tg.cameraID,
							"attempt", attempt, "err", err)
					})
			}
			if capErr != nil {
				w.log.Warn("gated capture (camera) failed",
					"cycle_id", cycleID, "tank_id", tankID, "camera_id", tg.cameraID, "err", capErr)
				return
			}
			durS := int(time.Since(actualStart).Seconds())
			if durS < 1 {
				durS = 1
			}
			if lateBy := actualStart.Sub(startedAt); lateBy >= time.Second {
				w.log.Warn("gated capture 지연 시작 — 재시도로 회복",
					"cycle_id", cycleID, "tank_id", tankID, "camera_id", tg.cameraID,
					"late_by_s", int(lateBy.Seconds()))
			}
			result := &Result{
				CycleID:    cycleID,
				ClipID:     clipID,
				TankID:     tankID,
				CameraID:   tg.cameraID,
				MP4Path:    mp4Path,
				DurationS:  durS,
				CapturedAt: actualStart,
				Phase:      PhaseFeeding,
				Reason:     ReasonFeedCycleGated,
			}
			w.log.Info("gated capture completed",
				"cycle_id", cycleID, "tank_id", tankID, "camera_id", tg.cameraID,
				"mp4_path", mp4Path, "duration_s", durS)
			if w.onResult != nil {
				// 녹화 ctx 는 이미 취소됨 → 적재/보강 조회는 fresh ctx 로.
				octx, ocancel := context.WithTimeout(context.Background(), gatedOnResultTimeout)
				w.onResult(octx, result)
				ocancel()
			}
		}(tg)
	}

	w.recMu.Lock()
	if w.recordings == nil {
		w.recordings = make(map[string]*cycleRecording)
	}
	if old := w.recordings[cycleID]; old != nil {
		old.cancel() // 방어 — 같은 cycleID 잔존 녹화 정리
	}
	w.recordings[cycleID] = rec
	w.recMu.Unlock()

	w.log.Info("gated capture started", "cycle_id", cycleID, "tank_id", tankID, "cameras", len(targets))
	if w.onCycleRecStart != nil {
		w.onCycleRecStart(cycleID, tankID)
	}
	return nil
}

// StopCycleRecording — 신호게이팅 녹화 종료(마지막 OFF/finalizeCycle). 녹화를 취소하고
// 모든 카메라 clip 이 clean EOS + onResult 발화될 때까지 대기한다. ⚠ 사이클 deregister 전에
// 호출해야 onResult 의 evidence 보강(feed_target/total)이 active 사이클을 찾는다.
func (w *Worker) StopCycleRecording(_ context.Context, cycleID string) {
	w.recMu.Lock()
	rec := w.recordings[cycleID]
	delete(w.recordings, cycleID)
	w.recMu.Unlock()
	if rec == nil {
		return
	}
	if w.onCycleRecStop != nil {
		w.onCycleRecStop(cycleID, rec.tankID)
	}
	rec.cancel()
	rec.wg.Wait()
	w.log.Info("gated capture stopped", "cycle_id", cycleID)
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
	resolveCamera func(context.Context, string) ([]CameraRef, error),
	decidePhase func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string),
) ([]*Result, error) {
	// StartContinuous(버튼)가 cfg.Continuous 를 무시하므로 여기서도 Enabled 만 확인한다.
	// (cfg.Continuous 는 부팅 자동시작 여부만 결정 — RunContinuous 에서 처리.)
	if !w.cfg.Enabled {
		return nil, nil
	}
	if err := os.MkdirAll(w.cfg.TempDir, 0o755); err != nil {
		return nil, fmt.Errorf("capture temp dir: %w", err)
	}

	startTS := time.Now().UTC()
	tsPart := strings.ReplaceAll(startTS.Format("20060102T150405.000Z"), ".", "_") // 파일명 안전

	phase := PhaseBaseline
	cycleID := ""
	if decidePhase != nil {
		phase, cycleID = decidePhase(ctx, tankID, startTS)
		if phase == "" {
			phase = PhaseBaseline
		}
	}

	// 실조도 야간 휴면 — baseline 상시 캡처만 게이트. feeding(사이클 중) 클립과
	// 신호게이팅 사이클 녹화는 게이트 대상이 아니다.
	if phase != PhaseFeeding && strings.ToLower(w.cfg.Mode) == ModeRTSP &&
		w.darkSleepShouldSkip(tankID, time.Now()) {
		return nil, nil
	}

	// 캡처 대상 목록. rtsp 모드는 수조의 모든 카메라(각각 crop 적용)를 개별 캡처 →
	// 수조·카메라별로 분리된 mp4. fixture 모드는 카메라 개념 없이 tank 당 1개.
	type target struct {
		cameraID  string
		rtspURL   string
		cropLeft  float64
		cropRight float64
	}
	var targets []target

	switch strings.ToLower(w.cfg.Mode) {
	case ModeFixture:
		if w.cfg.FixturePath == "" {
			return nil, fmt.Errorf("capture mode=fixture but fixture_path is empty")
		}
		targets = []target{{cameraID: "fixture"}}
	case ModeRTSP:
		if resolveCamera == nil {
			return nil, fmt.Errorf("capture continuous mode=rtsp but resolveCamera is nil")
		}
		refs, err := resolveCamera(ctx, tankID)
		if err != nil {
			return nil, fmt.Errorf("camera resolve for tank %s: %w", tankID, err)
		}
		for _, r := range refs {
			targets = append(targets, target{cameraID: r.CameraID, rtspURL: r.RTSPURL, cropLeft: r.CropLeft, cropRight: r.CropRight})
		}
	default:
		return nil, fmt.Errorf("capture mode %q unsupported", w.cfg.Mode)
	}

	var results []*Result
	var firstErr error
	for _, tg := range targets {
		// 파일명에 camera_id 포함 → 수조·카메라별 분리(크롭공유 카메라도 각각 별도 파일).
		safeCam := strings.ReplaceAll(tg.cameraID, "/", "_")
		clipID := fmt.Sprintf("clip_%s_%s_%s", tankID, safeCam, tsPart)
		mp4Path := filepath.Join(w.cfg.TempDir, clipID+".mp4")

		if strings.ToLower(w.cfg.Mode) == ModeFixture {
			if err := copyFile(w.cfg.FixturePath, mp4Path); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		} else {
			if err := captureRTSP(ctx, tg.rtspURL, mp4Path, w.cfg.DurationSeconds, tg.cropLeft, tg.cropRight); err != nil {
				w.log.Warn("continuous capture (camera) failed", "tank_id", tankID, "camera_id", tg.cameraID, "err", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}

		w.log.Info("continuous capture completed",
			"clip_id", clipID, "tank_id", tankID, "camera_id", tg.cameraID,
			"mp4_path", mp4Path, "phase", phase, "cycle_id", cycleID, "mode", w.cfg.Mode)

		results = append(results, &Result{
			ClipID:     clipID,
			CycleID:    cycleID,
			TankID:     tankID,
			CameraID:   tg.cameraID,
			MP4Path:    mp4Path,
			DurationS:  w.cfg.DurationSeconds,
			CapturedAt: startTS,
			Phase:      phase,
		})
	}

	// 실조도 게이트 판정 — 이번 라운드 카메라들 중 최대 luma 로 상태 갱신.
	// 어두우면(휴면) 클립 폐기. feeding 클립은 항상 배포.
	if phase != PhaseFeeding && strings.ToLower(w.cfg.Mode) == ModeRTSP && len(results) > 0 {
		maxLuma, lumaOK := -1.0, false
		for _, r := range results {
			if l, c, lerr := clipLumaChroma(r.MP4Path); lerr == nil {
				lumaOK = true
				if c < captureIRChroma {
					l = 0 // 무채색 = IR 야간 모드/소등 — 밝아도 어두움으로 취급
				}
				if l > maxLuma {
					maxLuma = l
				}
			}
		}
		if !w.lightGateDispatch(tankID, maxLuma, lumaOK) {
			// 야간 콜백이 있으면 클립을 넘긴다 (활동성 측정 + 파일 삭제까지 콜백 담당).
			// 학습 풀(media.clip.stored)에는 여전히 미적재 — onResult 는 타지 않는다.
			for _, r := range results {
				if w.onNightClip != nil {
					w.onNightClip(ctx, r)
				} else {
					_ = os.Remove(r.MP4Path)
					_ = os.Remove(strings.TrimSuffix(r.MP4Path, ".mp4") + ".jpg")
				}
			}
			w.log.Info("continuous capture discarded (dark-sleep)",
				"tank_id", tankID, "clips", len(results), "luma", fmt.Sprintf("%.1f", maxLuma),
				"night_metered", w.onNightClip != nil)
			return nil, nil
		}
	}

	for _, r := range results {
		if w.onResult != nil {
			w.onResult(ctx, r)
		}
	}
	if len(results) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// darkSleepShouldSkip — 야간 휴면 중이고 프로브 주기 전이면 true (캡처 생략).
// 프로브 시각이 되면 lastProbe 갱신 후 false — 이번 캡처가 재점등 확인 프로브가 된다.
func (w *Worker) darkSleepShouldSkip(tankID string, now time.Time) bool {
	w.lightMu.Lock()
	defer w.lightMu.Unlock()
	st := w.light[tankID]
	if st == nil || !st.sleeping {
		return false
	}
	if now.Sub(st.lastProbe) < captureProbeEvery {
		return true
	}
	st.lastProbe = now
	return false
}

// lightGateDispatch — 이번 라운드의 최대 luma 관측으로 조도 상태를 갱신하고,
// 캡처된 클립을 배포할지(true)/폐기할지(false) 반환. lumaOK=false(측정 실패)는
// 상태 유지 (깨어 있으면 fail-open 배포, 휴면이면 계속 폐기).
func (w *Worker) lightGateDispatch(tankID string, maxLuma float64, lumaOK bool) bool {
	w.lightMu.Lock()
	defer w.lightMu.Unlock()
	if w.light == nil {
		w.light = map[string]*tankLightState{}
	}
	st := w.light[tankID]
	if st == nil {
		st = &tankLightState{}
		w.light[tankID] = st
	}
	if !lumaOK {
		return !st.sleeping
	}
	switch {
	case maxLuma < captureDarkLuma:
		st.darkStreak++
		if !st.sleeping && st.darkStreak >= captureDarkStreak {
			st.sleeping = true
			st.lastProbe = time.Now()
			w.log.Info("continuous capture dark-sleep — 소등 감지",
				"tank_id", tankID, "luma", fmt.Sprintf("%.1f", maxLuma))
		}
		return !st.sleeping
	case maxLuma >= captureWakeLuma:
		if st.sleeping {
			w.log.Info("continuous capture resumed — 점등 감지",
				"tank_id", tankID, "luma", fmt.Sprintf("%.1f", maxLuma))
		}
		st.sleeping = false
		st.darkStreak = 0
		return true
	default:
		// 중간 대역(35~50) — 히스테리시스: 상태 유지, streak 리셋.
		st.darkStreak = 0
		return !st.sleeping
	}
}

// captureIRChroma — 평균 채도(max-min 채널차)가 이 미만이면 무채색 = IR 야간
// 모드/소등으로 간주. 실측 (2026-07-08): 야간 IR 0.0 / 야간 수중(컬러) 3.7 /
// 주간 25~30 — CCTV 가 야간에 IR 로 전환하면 밝은 흑백이 나와 luma 만으로는
// 소등을 못 잡는다.
const captureIRChroma = 5.0

// clipLumaChroma — 클립 포스터 프레임(<clip>.jpg)의 평균 휘도 + 평균 채도.
// vision_pipeline.meanLuma 와 동일 계열 공식 — vision_pipeline 이 capture 를
// import 하므로(순환 방지) 소형 헬퍼를 여기 둔다.
func clipLumaChroma(mp4Path string) (luma, chroma float64, err error) {
	data, err := os.ReadFile(strings.TrimSuffix(mp4Path, ".mp4") + ".jpg")
	if err != nil {
		return 0, 0, err
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	b := img.Bounds()
	const stride = 8
	var lumaSum, chromaSum float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y += stride {
		for x := b.Min.X; x < b.Max.X; x += stride {
			r, g, bl, _ := img.At(x, y).RGBA()
			rf, gf, bf := float64(r>>8), float64(g>>8), float64(bl>>8)
			lumaSum += 0.299*rf + 0.587*gf + 0.114*bf
			mx, mn := rf, rf
			for _, v := range []float64{gf, bf} {
				if v > mx {
					mx = v
				}
				if v < mn {
					mn = v
				}
			}
			chromaSum += mx - mn
			n++
		}
	}
	if n == 0 {
		return 0, 0, fmt.Errorf("empty image bounds")
	}
	return lumaSum / float64(n), chromaSum / float64(n), nil
}

// RunContinuous — R6.1 상시 캡처 loop. 모든 ContinuousTanks 에 대해 병렬 goroutine.
// ctx cancel 시 graceful 종료.
//
// 각 tank goroutine 패턴: capture → spacing 만큼 대기 → 반복.
// spacing=0 이면 DurationSeconds 그대로 (back-to-back). 작은 spacing 으로 CPU 부담 조절 가능.
//
// 호출자가 resolveCamera 와 decidePhase 콜백을 제공. capture 가 runtime/storage 에 import
// cycle 안 만들기 위한 의존성 역전.
// ConfigureRuntime registers the base context + resolve/phase callbacks so the
// continuous capture loop can be toggled at runtime (dashboard button).
// Call once at startup; StartContinuous/StopContinuous then control it live.
func (w *Worker) ConfigureRuntime(
	baseCtx context.Context,
	resolveCamera func(context.Context, string) ([]CameraRef, error),
	decidePhase func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string),
) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.baseCtx = baseCtx
	w.resolveCamera = resolveCamera
	w.decidePhase = decidePhase
}

// IsRunning reports whether the continuous capture loop is active.
func (w *Worker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// StartContinuous begins the continuous capture loop at runtime. Unlike the
// config-gated RunContinuous, it ignores cfg.Continuous (the button is the
// trigger) but still requires Enabled + a non-empty tank list + ConfigureRuntime.
func (w *Worker) StartContinuous() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	if !w.cfg.Enabled {
		return fmt.Errorf("capture disabled in config")
	}
	if len(w.cfg.ContinuousTanks) == 0 {
		return fmt.Errorf("continuous_tanks is empty")
	}
	if w.baseCtx == nil {
		return fmt.Errorf("capture runtime not configured")
	}
	ctx, cancel := context.WithCancel(w.baseCtx)
	w.cancel = cancel
	w.running = true
	w.launchLoops(ctx, w.resolveCamera, w.decidePhase)
	return nil
}

// StopContinuous halts the continuous capture loop.
func (w *Worker) StopContinuous() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.running = false
	w.log.Info("continuous capture stopped by operator")
}

// RunContinuous — config-gated auto-start at boot. Registers runtime callbacks
// then starts the loop only when cfg.Continuous=true. Runtime button uses
// StartContinuous/StopContinuous.
func (w *Worker) RunContinuous(
	ctx context.Context,
	resolveCamera func(context.Context, string) ([]CameraRef, error),
	decidePhase func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string),
) error {
	w.ConfigureRuntime(ctx, resolveCamera, decidePhase)
	if !w.cfg.Enabled || !w.cfg.Continuous {
		return nil
	}
	if len(w.cfg.ContinuousTanks) == 0 {
		w.log.Warn("continuous mode enabled but continuous_tanks is empty — nothing to capture")
		return nil
	}
	w.launchCleanup(ctx)
	return w.StartContinuous()
}

// launchCleanup — retention 정리 goroutine. 2026-08-18 배선.
//
// CleanupOnce 는 오래 전부터 구현돼 있었으나 **호출하는 곳이 없어** 상시 클립이
// 무한 누적됐다 — 8-18 실측 157,038개 · 273 GB(디스크 91%, 잔여 11일). 상시 캡처는
// 하루 6 GB 씩 쌓이므로 한 번 지워도 배선이 없으면 한 달 뒤 같은 상황이 온다.
//
// ⚠ cleanupMP4Dir 이 보존하는 것은 cycle_ prefix(급이 클립)뿐이다. 라벨이 붙은
// 상시 클립(clip_ prefix, R7 baseline 228건 + 격리 58건)은 시간만 지나면 지워지므로
// captures/preserved/ 하위로 옮겨 보호한다 — cleanupMP4Dir 은 디렉터리를 건너뛴다.
func (w *Worker) launchCleanup(ctx context.Context) {
	if w.cfg.RetentionMinutes <= 0 && w.cfg.MaxDiskPercent <= 0 {
		return // 둘 다 비활성 — 정리 안 함
	}
	go func() {
		// 기동 직후 1회 — 재부팅으로 정리가 밀린 분을 즉시 회수.
		if err := w.CleanupOnce(); err != nil {
			w.log.Warn("capture cleanup failed", "err", err)
		}
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.CleanupOnce(); err != nil {
					w.log.Warn("capture cleanup failed", "err", err)
				}
			}
		}
	}()
}

// launchLoops spawns one capture goroutine per continuous tank. Caller holds w.mu.
func (w *Worker) launchLoops(
	ctx context.Context,
	resolveCamera func(context.Context, string) ([]CameraRef, error),
	decidePhase func(ctx context.Context, tankID string, capturedAt time.Time) (phase, cycleID string),
) {
	spacing := time.Duration(w.cfg.ContinuousModeSpacing) * time.Second
	if spacing <= 0 {
		spacing = time.Duration(w.cfg.DurationSeconds) * time.Second
	}
	filter := w.activeTank // caller 가 w.mu 보유 — 안전하게 로컬 캡처
	w.log.Info("continuous capture loop starting",
		"tanks", w.cfg.ContinuousTanks, "duration_s", w.cfg.DurationSeconds, "spacing", spacing)

	nTanks := len(w.cfg.ContinuousTanks)
	for idx, tankID := range w.cfg.ContinuousTanks {
		t := tankID // capture
		// 수조별 시차 시작 — 여러 수조의 RTSP 연결이 동시에 몰려 셋업이 느려지는(=clip 짧아지는)
		// 경합을 완화. spacing 을 수조 수로 균등 분할해 시작 시점을 분산.
		offset := time.Duration(idx) * spacing / time.Duration(nTanks)
		go func() {
			if offset > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(offset):
				}
			}
			ticker := time.NewTicker(spacing)
			defer ticker.Stop()
			// 입식 게이트: 미입식(fish_count 0/미설정) 수조는 skip. 매 tick 재평가 → 동적.
			doCapture := func() {
				if filter != nil && !filter(ctx, t) {
					return // 빈 수조 — 캡처 skip
				}
				if _, err := w.CaptureContinuous(ctx, t, resolveCamera, decidePhase); err != nil {
					w.log.Warn("continuous capture failed", "tank_id", t, "err", err)
				}
			}
			doCapture() // 첫 회 즉시
			for {
				select {
				case <-ctx.Done():
					w.log.Info("continuous capture loop stopped", "tank_id", t)
					return
				case <-ticker.C:
					doCapture()
				}
			}
		}()
	}
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
		// 실행 시각·소요·삭제 건수를 남긴다 (2026-08-19). 종전에는 성공 시 조용해서
		// mp4 마무리 실패(moov 미기록)와 시간 대조가 불가능했다 — 8-19 11:50·13:43
		// 사이클의 클립이 열리지 않았고 cleanup 경합이 후보였으나 확인할 자료가 없었다.
		// 25만 파일 규모 디렉터리 스캔이라 소요 시간 자체가 관측 대상이다.
		started := time.Now()
		cutoff := started.Add(-time.Duration(w.cfg.RetentionMinutes) * time.Minute)
		w.log.Info("capture cleanup start", "retention_min", w.cfg.RetentionMinutes,
			"cutoff", cutoff.Format(time.RFC3339), "dir", w.cfg.TempDir)
		// main TempDir — 급이(cycle_) 클립은 학습 데이터라 보존, baseline(clip_) 만 정리.
		removed, err := cleanupMP4Dir(w.cfg.TempDir, cutoff, true)
		w.log.Info("capture cleanup done", "removed", removed,
			"elapsed_ms", time.Since(started).Milliseconds(), "err", err)
		if err != nil {
			return err
		}
		// excluded/ 하위 recursive — 운영자 격리 영상이므로 급이 포함 정상 정리.
		excludedRoot := filepath.Join(w.cfg.TempDir, "excluded")
		if entries, err := os.ReadDir(excludedRoot); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if n, _ := cleanupMP4Dir(filepath.Join(excludedRoot, e.Name()), cutoff, false); n > 0 {
					w.log.Info("capture cleanup excluded", "dir", e.Name(), "removed", n)
				}
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
			// poster 프레임(.jpg)도 함께 정리 — mp4 삭제 시 고아 방지.
			_ = os.Remove(strings.TrimSuffix(f.path, ".mp4") + ".jpg")
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

// cleanupMP4Dir — dir 안의 cutoff 초과 mp4 삭제.
// preserveFeeding=true 이면 급이 클립(cycle_ prefix)은 시간 retention 에서 제외한다.
// 급이 클립은 LRCN 학습 데이터(희소·귀중)라 상시 baseline(clip_) 과 달리 보존한다.
// (excluded/ 하위는 운영자가 격리한 것이므로 preserveFeeding=false 로 정상 정리.)
func cleanupMP4Dir(dir string, cutoff time.Time, preserveFeeding bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".mp4" {
			continue
		}
		if preserveFeeding && strings.HasPrefix(e.Name(), "cycle_") {
			continue // 급이(학습) 클립 보존
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			p := filepath.Join(dir, e.Name())
			_ = os.Remove(p)
			// poster 프레임(.jpg)도 함께 정리 — mp4 삭제 시 고아 방지.
			_ = os.Remove(strings.TrimSuffix(p, ".mp4") + ".jpg")
			removed++
		}
	}
	return removed, nil
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

// gstRTSPArgs — RTSP → mp4 gst-launch 파이프라인 인자. captureRTSP(고정길이)와
// captureRTSPUntil(신호게이팅) 공용.
// RTSP 는 GOP 중간에서 붙으므로 첫 키프레임(IDR) 도착 전 P-프레임이 참조 없이 회색 잔상
// (garbage)으로 디코드된다. avdec_h264 output-corrupt=false 로 그 프레임들을 버려 클립이
// 정상 프레임부터 시작하게 한다(fps·화질 영향 없음, 학습데이터·썸네일 오염 방지).
// 항상 decode → 정상프레임만 → (크롭공유면 videocrop) → x264 재인코딩.
func gstRTSPArgs(rtspURL, outPath string, cropLeft, cropRight float64) []string {
	args := []string{"-q", "-e",
		"rtspsrc", "location=" + rtspURL, "latency=800", "protocols=tcp",
		"!", "rtph264depay", "!", "h264parse",
		"!", "avdec_h264", "output-corrupt=false",
		"!", "videoconvert"}
	if cropLeft > 0 || cropRight > 0 {
		// 크롭공유 카메라(한 화면 2수조)를 수조별로 분리. 폭 640 기준으로 crop 픽셀 계산.
		const cropWidth = 640
		left := int(cropLeft * float64(cropWidth))
		right := int(cropRight * float64(cropWidth))
		args = append(args,
			"!", "videoscale",
			"!", fmt.Sprintf("video/x-raw,width=%d", cropWidth),
			"!", "videocrop", fmt.Sprintf("left=%d", left), fmt.Sprintf("right=%d", right))
	}
	return append(args,
		"!", "x264enc", "speed-preset=ultrafast", "tune=zerolatency", "key-int-max=15",
		"!", "mp4mux",
		"!", "filesink", "location="+outPath)
}

// captureRTSPUntil — RTSP → mp4 를 ctx 종료(정지 신호 또는 백스톱 deadline)까지 연속 녹화.
// 신호게이팅 급이 캡처용: 사이클 첫 ON~마지막 OFF 구간을 1개 긴 clip 으로. captureRTSP 와
// 동일 파이프라인이나 고정 시간 대신 ctx 취소로 SIGINT clean EOS.
func captureRTSPUntil(ctx context.Context, rtspURL, outPath string, cropLeft, cropRight float64) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return fmt.Errorf("gst-launch-1.0 is not installed")
	}
	if rtspURL == "" {
		return fmt.Errorf("rtsp_url is required")
	}
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", gstRTSPArgs(rtspURL, outPath, cropLeft, cropRight)...)
	// -e + SIGINT → mp4mux clean EOS(moov 정상). WaitDelay 후 SIGKILL 백스톱.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGINT) }
	cmd.WaitDelay = 3 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	// ctx 취소/deadline 로 SIGINT 종료 → err 는 killed 로 non-nil 이나 mp4 는 유효.
	if info, statErr := os.Stat(outPath); statErr == nil && info.Size() > 0 {
		if jpg, perr := extractPosterFrame(outPath); perr != nil {
			slog.Warn("poster frame extract failed", "mp4", outPath, "error", perr)
		} else {
			slog.Debug("poster frame saved", "jpg", jpg)
		}
		return nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" && err != nil {
		msg = err.Error()
	}
	return fmt.Errorf("rtsp gated capture failed: %s", msg)
}

// captureRTSP — gst-launch-1.0 으로 RTSP → mp4 N초 캡처. snapshotJPEG (internal/api/cameras.go:580) 와 같은 패턴.
// SIGTERM 으로 graceful 종료해야 mp4mux 가 깨끗하게 close (SIGKILL 시 mp4 손상).
func captureRTSP(parent context.Context, rtspURL, outPath string, durationSec int, cropLeft, cropRight float64) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return fmt.Errorf("gst-launch-1.0 is not installed")
	}
	if rtspURL == "" {
		return fmt.Errorf("rtsp_url is required")
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(durationSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", gstRTSPArgs(rtspURL, outPath, cropLeft, cropRight)...)
	// gst-launch -e 는 SIGINT 에서 clean EOS → mp4mux 가 moov 를 정상 기록.
	// SIGTERM 은 finalize 실패(재생 불가 mp4)라 SIGINT 사용. WaitDelay 후 SIGKILL 백스톱.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGINT) }
	cmd.WaitDelay = 3 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	// 정상 완료 판정: 자연 EOS(err=nil) 또는 durationSec 후 SIGINT(ctx deadline)로 clean EOS.
	success := err == nil
	if !success && ctx.Err() == context.DeadlineExceeded {
		if info, statErr := os.Stat(outPath); statErr == nil && info.Size() > 0 {
			success = true
		}
	}
	if !success {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("rtsp capture failed: %s", msg)
	}
	// 저장된 clip 에서 poster 프레임 1장을 <clip>.jpg 로 추출 (썸네일·미리보기·라벨링용).
	// best-effort — 실패해도 clip 저장은 유효하므로 warn 만.
	if jpg, perr := extractPosterFrame(outPath); perr != nil {
		slog.Warn("poster frame extract failed", "mp4", outPath, "error", perr)
	} else {
		slog.Debug("poster frame saved", "jpg", jpg)
	}
	return nil
}

// CaptureJudgeClip — 한 펄스의 배출판정용 짧은 탑뷰 클립 1개를 즉석 캡처(블로킹).
// r3 섀도 판정(feed_cycle) 전용: 사이클 학습 클립(StartCycleRecording)과 별개로 gap 반응
// 창을 담는 durationSec 고정길이 mp4 를 만든다. resolveCamera 로 그 수조의 카메라를 받아
// 탑뷰(camera_id 규칙: "top" 포함·"underwater" 제외 — reporter.scoreSegments 와 동일)를
// 고른다. camera_profiles.view_angle 은 현재 공란이라 id 규칙을 쓴다. 반환 path 는 호출부가
// LRCN 채점 후 사용하며, 파일 정리는 capture retention 이 담당한다. mode=rtsp 전용.
func (w *Worker) CaptureJudgeClip(ctx context.Context, tankID string, durationSec int) (mp4Path, cameraID string, err error) {
	if !w.cfg.Enabled {
		return "", "", fmt.Errorf("capture disabled")
	}
	if strings.ToLower(w.cfg.Mode) != ModeRTSP {
		return "", "", fmt.Errorf("judge clip requires capture mode=rtsp (got %q)", w.cfg.Mode)
	}
	if w.resolveCamera == nil {
		return "", "", fmt.Errorf("judge clip: resolveCamera not configured")
	}
	if durationSec <= 0 {
		return "", "", fmt.Errorf("judge clip: durationSec must be > 0")
	}
	refs, err := w.resolveCamera(ctx, tankID)
	if err != nil {
		return "", "", fmt.Errorf("judge clip: camera resolve for tank %s: %w", tankID, err)
	}
	var top *CameraRef
	for i := range refs {
		id := strings.ToLower(refs[i].CameraID)
		if strings.Contains(id, "underwater") {
			continue
		}
		if strings.Contains(id, "top") {
			top = &refs[i]
			break
		}
	}
	if top == nil {
		return "", "", fmt.Errorf("judge clip: no top-view camera for tank %s", tankID)
	}
	if err := os.MkdirAll(w.cfg.TempDir, 0o755); err != nil {
		return "", "", fmt.Errorf("judge clip temp dir: %w", err)
	}
	outPath := filepath.Join(w.cfg.TempDir,
		fmt.Sprintf("judge_%s_%s_%d.mp4", tankID, top.CameraID, time.Now().UTC().UnixNano()))
	if err := captureRTSP(ctx, top.RTSPURL, outPath, durationSec, top.CropLeft, top.CropRight); err != nil {
		return "", "", fmt.Errorf("judge clip capture: %w", err)
	}
	return outPath, top.CameraID, nil
}

// extractPosterFrame — 저장된 mp4 에서 첫 프레임 1장을 <clip>.jpg 로 뽑는다.
// jpegenc snapshot=true 는 1프레임 인코딩 후 EOS → 파일 1장만 기록. output-corrupt=false
// fix 로 첫 프레임은 정상 영상이다. 항상 fresh context(호출부 취소 무관)로 발행.
func extractPosterFrame(mp4Path string) (string, error) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return "", err
	}
	jpgPath := strings.TrimSuffix(mp4Path, ".mp4") + ".jpg"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q",
		"filesrc", "location="+mp4Path,
		"!", "qtdemux", "!", "h264parse", "!", "avdec_h264",
		"!", "videoconvert", "!", "jpegenc", "snapshot=true",
		"!", "filesink", "location="+jpgPath)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if info, err := os.Stat(jpgPath); err != nil || info.Size() == 0 {
		return "", fmt.Errorf("poster jpg not written")
	}
	return jpgPath, nil
}
