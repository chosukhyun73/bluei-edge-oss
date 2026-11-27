package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"bluei.kr/edge/internal/media"
	"bluei.kr/edge/internal/storage"
)

type cameraJPEGWorker struct {
	CameraID string
	Tier     string
	TankID   string
	RTSPURL  string
	// 수조별 크롭 (공용 탑뷰 1대를 좌/우로 나눠 각 수조 단일 프레임 제공 — AI 관찰용).
	// crop_left / crop_right = 폭 대비 각 변에서 잘라낼 비율(0~1). metadata 에서 읽음.
	CropLeft  float64
	CropRight float64
	Hub       *media.FrameHub
	cmd       *exec.Cmd
	started   time.Time
	done      chan struct{}
	// stop — 유휴 회수(reapIdleCameraHubs)가 닫는다. 닫히면 파이프라인을 죽이고
	// 루프가 리턴해 done 이 닫힌다 → workerAlive=false → 다음 요청 때 새로 생성.
	stop chan struct{}
	// lastAccessUnix — 마지막으로 이 워커가 필요해진 시각(unix sec). ensureCameraJPEGWorker
	// 가 호출될 때마다(신규·재사용 모두) 갱신된다. keepalive 가 입식 수조를 20초마다
	// ensure 하므로 관찰 대상 수조는 항상 최신이고, 아무도 찾지 않는 워커만 낡는다.
	lastAccessUnix atomic.Int64
	// lastRealFrameUnix — **실제 프레임을 받은** 마지막 시각(unix sec). 0 = 아직 한 장도 못 받음.
	//
	// lastFrameUnix 와 반드시 구분한다. 그쪽은 파이프라인을 새로 띄울 때마다 현재 시각으로
	// 재장전되는데(워치독이 기동 중인 파이프라인을 즉시 죽이지 않게 하려는 의도), 상류가
	// 죽어 재연결 루프가 도는 동안에도 계속 갱신된다. 비전 게이트가 그 값을 보면 단절
	// 중에도 "방금 프레임 받음"으로 읽혀 급이를 못 막는다 — 2026-08-27 22:48 실측에서
	// 카메라 5대가 전부 죽었는데 lastFrameUnix 는 1~18초 전이었다.
	lastRealFrameUnix atomic.Int64
	// lastFrameUnix — 프레임 갭 워치독용. 파이프라인 기동 시각으로도 재장전된다.
	// 파이프라인이 에러 없이 hang(오프라인 카메라 DESCRIBE 대기 등)하면 NextPart 가
	// 영원히 블록되어 reconnect 루프에 못 들어간다 — 워치독이 갭 감지 시 프로세스를 죽여 복귀.
	lastFrameUnix atomic.Int64
}

// cropFromMetadata reads crop_left / crop_right (0~1 fractions) from a camera's metadata.
func cropFromMetadata(m map[string]any) (left, right float64) {
	if m == nil {
		return 0, 0
	}
	return metaFloat(m["crop_left"]), metaFloat(m["crop_right"])
}

func metaFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

var cameraJPEGWorkers = struct {
	sync.Mutex
	items map[string]*cameraJPEGWorker
}{items: map[string]*cameraJPEGWorker{}}

// MaxViewersPerWorker — 운영자 dashboard 가 같은 카메라를 여러 위치 (개요/Cage/Tank 비교/TankDetail)
// 에서 동시 표시 + dev 환경 StrictMode/HMR 가 mount 두 배 → 4 가 너무 빠르게 소진.
// 16 으로 상향 — 실 운영자 < 5명 가정에서도 여유.
var cameraStreamLimits = media.StreamLimits{MaxWorkers: 8, MaxViewersPerWorker: 16}

const cameraJPEGTargetFPS = 10

// jpegFrameGapTimeout — 이 시간 동안 프레임이 없으면 워치독이 파이프라인을 재시작한다.
// RTSP 연결·키프레임 대기(수 초)보다 충분히 길고, 스트림 순단 복구는 빠르게.
const jpegFrameGapTimeout = 30 * time.Second

// cameraHubKeepaliveInterval — 상시 기동 점검 주기. 워커가 살아 있으면 no-op 이므로
// 짧아도 비용이 없고, 죽어 있으면 이 주기 안에 다시 살아난다.
const cameraHubKeepaliveInterval = 20 * time.Second

// cameraHubIdleTimeout — 아무도 찾지 않는 워커를 회수하기까지의 유휴 시간.
//
// keepalive 가 입식 수조(fish_count>0)를 매 tick ensure 하므로 관찰 대상 워커는
// 여기 걸리지 않는다. 걸리는 것은 **출하되어 대상에서 빠졌는데도 남아 있는 워커**와
// 대시보드가 잠깐 열어본 빈 수조 워커다. 종전에는 리턴 경로가 없어 한 번 뜬 워커가
// 프로세스 수명 내내 상류 연결을 물었다 — 2026-08-22 실측에서 빈 수조 tank_03·04 몫
// 2개가 상시 연결 5개 중 2개를 차지하고 있었다("출하가 바로 반영된다"는 종전 주석은
// 생성 방향에만 맞았다).
//
// keepalive 주기(20s)보다 충분히 길게 잡아 대상 수조가 경계에서 깜빡이지 않게 한다.
const cameraHubIdleTimeout = 3 * time.Minute

// StartCameraHubKeepalive — 입식 수조의 카메라 프레임허브를 뷰어와 무관하게 상시 유지한다.
//
// 허브 워커는 원래 스냅샷/MJPEG 요청이 올 때만 생성됐다(cameras.go). 대시보드가 꺼진
// 새벽에는 연결을 유지하는 주체가 아무도 없고, 30초마다 뜨는 단발 mp4 캡처만 남는다.
// 그 단발 연결은 duration 이 곧 타임아웃이라(capture.go:1072) 업스트림이 느려지면
// 붙지 못한 채 죽고, 재시도해도 조건이 같아 고착된다 — 2026-08-11 03:00~09:39 실측
// (6시간 39분 단절. 긴 예산으로 한 번 붙여주자 즉시 정상 복귀).
//
// 허브를 상시 띄우면 이미 있는 재연결 백오프(1s~15s)와 워치독(30s)이 항상 돌아
// 스스로 빠져나오고, 업스트림이 warm 하게 유지되어 캡처의 짧은 예산도 넉넉해진다.
//
// active 는 캡처와 같은 게이트(fish_count>0)를 받는다 — 빈 수조까지 상시 연결하지
// 않기 위해서다. 매 tick 재평가하므로 입식/출하가 바로 반영된다.
func (s *Server) StartCameraHubKeepalive(ctx context.Context, tanks []string, active func(context.Context, string) bool) {
	if len(tanks) == 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(cameraHubKeepaliveInterval)
		defer ticker.Stop()
		for {
			s.ensureTankHubs(ctx, tanks, active)
			reapIdleCameraHubs()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// ensureTankHubs — 대상 수조의 카메라마다 허브 워커를 보장한다. 살아 있으면 no-op.
// 개별 카메라 실패는 나머지를 막지 않는다 (한 대가 죽어도 다른 수조는 계속 관찰).
func (s *Server) ensureTankHubs(ctx context.Context, tanks []string, active func(context.Context, string) bool) {
	for _, tankID := range tanks {
		if active != nil && !active(ctx, tankID) {
			continue
		}
		profiles, err := s.store.ListCameraProfiles(ctx, tankID)
		if err != nil {
			slog.Warn("camera hub keepalive — 카메라 조회 실패", "tank_id", tankID, "error", err)
			continue
		}
		for _, p := range profiles {
			rtspURL, err := s.cameraRTSPURL(ctx, p, "sub")
			if err != nil {
				continue // URL 을 못 만드는 카메라(미설정)는 조용히 건너뛴다
			}
			if _, err := s.ensureCameraJPEGWorker(ctx, p, "sub", rtspURL); err != nil {
				slog.Warn("camera hub keepalive — 워커 기동 실패",
					"tank_id", tankID, "camera_id", p.CameraID, "error", err)
			}
		}
	}
}

func (s *Server) ensureCameraJPEGWorker(ctx context.Context, profile *storage.CameraProfile, tier string, rtspURL string) (*cameraJPEGWorker, error) {
	if tier == "" {
		tier = "sub"
	}
	key := profile.CameraID + ":" + tier
	cameraJPEGWorkers.Lock()
	if existing := cameraJPEGWorkers.items[key]; existing != nil && workerAlive(existing) {
		existing.lastAccessUnix.Store(time.Now().Unix())
		cameraJPEGWorkers.Unlock()
		return existing, nil
	}
	if existing := cameraJPEGWorkers.items[key]; existing != nil {
		delete(cameraJPEGWorkers.items, key)
	}
	activeWorkers := 0
	for _, worker := range cameraJPEGWorkers.items {
		if workerAlive(worker) {
			activeWorkers++
		}
	}
	if !cameraStreamLimits.AllowWorker(activeWorkers) {
		cameraJPEGWorkers.Unlock()
		return nil, fmt.Errorf("camera stream worker limit reached: %d", activeWorkers)
	}
	cameraJPEGWorkers.Unlock()

	cropLeft, cropRight := cropFromMetadata(profile.Metadata)
	worker := &cameraJPEGWorker{
		CameraID:  profile.CameraID,
		Tier:      tier,
		TankID:    profile.TankID,
		RTSPURL:   rtspURL,
		CropLeft:  cropLeft,
		CropRight: cropRight,
		Hub:       media.NewFrameHub(2),
		started:   time.Now().UTC(),
		done:      make(chan struct{}),
		stop:      make(chan struct{}),
	}
	worker.lastAccessUnix.Store(time.Now().Unix())
	cameraJPEGWorkers.Lock()
	cameraJPEGWorkers.items[key] = worker
	cameraJPEGWorkers.Unlock()
	s.updateCameraStatus(profile.CameraID, profile.TankID, "starting", map[string]any{"mode": "jpeg_worker", "profile": tier, "target_fps": cameraJPEGTargetFPS})
	go s.runCameraJPEGWorker(worker)
	return worker, nil
}

func (s *Server) runCameraJPEGWorker(worker *cameraJPEGWorker) {
	defer close(worker.done)
	policy := media.ReconnectPolicy{BaseDelay: time.Second, MaxDelay: 15 * time.Second}
	attempt := 0
	frames := int64(0)
	for {
		if stopRequested(worker) {
			s.updateCameraStatus(worker.CameraID, worker.TankID, "stopped", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "reason": "idle", "frames": frames})
			return
		}
		cmd, stdout, stderr, err := startJPEGPipeline(worker.RTSPURL, worker.CropLeft, worker.CropRight)
		if err != nil {
			s.updateCameraStatus(worker.CameraID, worker.TankID, "reconnecting", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "last_error": err.Error(), "reconnect_count": attempt + 1, "frames": frames})
			time.Sleep(policy.Delay(attempt))
			attempt++
			continue
		}
		worker.cmd = cmd
		go io.Copy(io.Discard, stderr)
		// 프레임 갭 워치독 — jpegFrameGapTimeout 동안 프레임이 없으면 파이프라인 프로세스를
		// 죽여 readJPEGFrames 를 에러로 깨우고 reconnect 루프로 복귀시킨다.
		// (2026-07-04: 카메라 오프라인/플래핑 중 hang 된 워커가 복구 후에도 영구 stuck 재발 방지)
		worker.lastFrameUnix.Store(time.Now().Unix())
		stopWatch := make(chan struct{})
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stopWatch:
					return
				case <-worker.stop:
					// 유휴 회수 — 프레임이 정상으로 들어오는 중에도 깨워야 하므로
					// 워치독과 같은 방식으로 프로세스를 죽여 readJPEGFrames 를 되돌린다.
					_ = cmd.Process.Kill()
					return
				case <-t.C:
					if time.Since(time.Unix(worker.lastFrameUnix.Load(), 0)) > jpegFrameGapTimeout {
						_ = cmd.Process.Kill()
						return
					}
				}
			}
		}()
		err = s.readJPEGFrames(worker, stdout, &frames, attempt)
		close(stopWatch)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if stopRequested(worker) {
			// 회수로 깨어난 경우 — 재연결 백오프를 기다리지 않고 즉시 내려간다.
			s.updateCameraStatus(worker.CameraID, worker.TankID, "stopped", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "reason": "idle", "frames": frames})
			return
		}
		s.updateCameraStatus(worker.CameraID, worker.TankID, "reconnecting", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "last_error": err.Error(), "reconnect_count": attempt + 1, "frames": frames, "active_viewers": worker.Hub.SubscriberCount()})
		time.Sleep(policy.Delay(attempt))
		attempt++
	}
}

func startJPEGPipeline(rtspURL string, cropLeft, cropRight float64) (*exec.Cmd, io.Reader, io.Reader, error) {
	cmd := exec.CommandContext(context.Background(), "python3", "scripts/gst_mjpeg_runner.py")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	go func() {
		defer stdin.Close()
		cfg := map[string]any{
			"rtsp_url": rtspURL,
			"width":    640,
			"fps":      cameraJPEGTargetFPS,
			"quality":  75,
			"boundary": "blueiframe",
		}
		if cropLeft > 0 || cropRight > 0 {
			cfg["crop"] = map[string]any{"left": cropLeft, "right": cropRight}
		}
		_ = json.NewEncoder(stdin).Encode(cfg)
	}()
	return cmd, stdout, stderr, nil
}

func (s *Server) readJPEGFrames(worker *cameraJPEGWorker, stdout io.Reader, frames *int64, reconnectCount int) error {
	reader := multipart.NewReader(stdout, "blueiframe")
	for {
		part, err := reader.NextPart()
		if err != nil {
			return err
		}
		ctype := part.Header.Get("Content-Type")
		if ctype == "" {
			ctype = "image/jpeg"
		}
		if ctype != "image/jpeg" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		*frames++
		worker.lastFrameUnix.Store(time.Now().Unix())
		worker.lastRealFrameUnix.Store(time.Now().Unix()) // 비전 게이트용 — 실제 프레임만
		capturedAt := time.Now().UTC()
		_ = worker.Hub.Publish(media.Frame{CameraID: worker.CameraID, TankID: worker.TankID, Tier: worker.Tier, MimeType: "image/jpeg", CapturedAt: capturedAt, Data: data})
		if *frames == 1 || *frames%25 == 0 {
			s.updateCameraStatus(worker.CameraID, worker.TankID, "streaming", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "target_fps": cameraJPEGTargetFPS, "frames": *frames, "active_viewers": worker.Hub.SubscriberCount(), "reconnect_count": reconnectCount})
		}
	}
}

// EnsureCameraFrameHub — 카메라 JPEG 워커를 보장하고 FrameHub 를 반환한다.
// 서버 내부 소비자(라이브 요동 세션 등)용 — 뷰어 한도(AllowViewer)는 대시보드
// MJPEG 접속에만 적용하고 내부 구독에는 적용하지 않는다.
func (s *Server) EnsureCameraFrameHub(ctx context.Context, cameraID, tier string) (*media.FrameHub, error) {
	profile, err := s.store.GetCameraProfile(ctx, cameraID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("camera profile not found: %s", cameraID)
	}
	rtspURL, err := s.cameraRTSPURL(ctx, profile, tier)
	if err != nil {
		return nil, err
	}
	worker, err := s.ensureCameraJPEGWorker(ctx, profile, tier, rtspURL)
	if err != nil {
		return nil, err
	}
	return worker.Hub, nil
}

func workerAlive(worker *cameraJPEGWorker) bool {
	select {
	case <-worker.done:
		return false
	default:
		return true
	}
}

func stopRequested(worker *cameraJPEGWorker) bool {
	select {
	case <-worker.stop:
		return true
	default:
		return false
	}
}

// reapIdleCameraHubs — 아무도 찾지 않는 프레임허브 워커를 내린다.
//
// 회수 대상은 **뷰어가 없고** cameraHubIdleTimeout 동안 ensureCameraJPEGWorker 호출이
// 한 번도 없었던 워커다. keepalive 가 입식 수조를 매 tick ensure 하므로 관찰 대상은
// 절대 걸리지 않고, 대시보드가 보고 있는 카메라도 스냅샷·MJPEG 요청이 시각을 갱신해
// 유지된다. 상시 유지 설계(2026-08-11 6시간 39분 단절 대응)는 그대로다.
func reapIdleCameraHubs() {
	cutoff := time.Now().Add(-cameraHubIdleTimeout).Unix()
	cameraJPEGWorkers.Lock()
	defer cameraJPEGWorkers.Unlock()
	for key, worker := range cameraJPEGWorkers.items {
		if !workerAlive(worker) {
			delete(cameraJPEGWorkers.items, key)
			continue
		}
		if stopRequested(worker) || worker.Hub.SubscriberCount() > 0 || worker.lastAccessUnix.Load() > cutoff {
			continue
		}
		close(worker.stop)
		delete(cameraJPEGWorkers.items, key)
		slog.Info("camera hub reaped — 유휴 워커 회수",
			"camera_id", worker.CameraID, "tank_id", worker.TankID, "tier", worker.Tier,
			"idle_sec", time.Now().Unix()-worker.lastAccessUnix.Load())
	}
}

func writeMultipartJPEG(w http.ResponseWriter, frame media.Frame) error {
	if _, err := fmt.Fprintf(w, "--blueiframe\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame.Data)); err != nil {
		return err
	}
	if _, err := w.Write(frame.Data); err != nil {
		return err
	}
	_, err := w.Write([]byte("\r\n"))
	return err
}
