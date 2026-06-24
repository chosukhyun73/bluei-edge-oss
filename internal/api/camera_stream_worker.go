package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"bluei.kr/edge/internal/media"
	"bluei.kr/edge/internal/storage"
)

type cameraJPEGWorker struct {
	CameraID string
	Tier     string
	TankID   string
	RTSPURL  string
	Hub      *media.FrameHub
	cmd      *exec.Cmd
	started  time.Time
	done     chan struct{}
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

func (s *Server) ensureCameraJPEGWorker(ctx context.Context, profile *storage.CameraProfile, tier string, rtspURL string) (*cameraJPEGWorker, error) {
	if tier == "" {
		tier = "sub"
	}
	key := profile.CameraID + ":" + tier
	cameraJPEGWorkers.Lock()
	if existing := cameraJPEGWorkers.items[key]; existing != nil && workerAlive(existing) {
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

	worker := &cameraJPEGWorker{
		CameraID: profile.CameraID,
		Tier:     tier,
		TankID:   profile.TankID,
		RTSPURL:  rtspURL,
		Hub:      media.NewFrameHub(2),
		started:  time.Now().UTC(),
		done:     make(chan struct{}),
	}
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
		cmd, stdout, stderr, err := startJPEGPipeline(worker.RTSPURL)
		if err != nil {
			s.updateCameraStatus(worker.CameraID, worker.TankID, "reconnecting", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "last_error": err.Error(), "reconnect_count": attempt + 1, "frames": frames})
			time.Sleep(policy.Delay(attempt))
			attempt++
			continue
		}
		worker.cmd = cmd
		go io.Copy(io.Discard, stderr)
		err = s.readJPEGFrames(worker, stdout, &frames, attempt)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		s.updateCameraStatus(worker.CameraID, worker.TankID, "reconnecting", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "last_error": err.Error(), "reconnect_count": attempt + 1, "frames": frames, "active_viewers": worker.Hub.SubscriberCount()})
		time.Sleep(policy.Delay(attempt))
		attempt++
	}
}

func startJPEGPipeline(rtspURL string) (*exec.Cmd, io.Reader, io.Reader, error) {
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
		_ = json.NewEncoder(stdin).Encode(map[string]any{
			"rtsp_url": rtspURL,
			"width":    640,
			"fps":      cameraJPEGTargetFPS,
			"quality":  75,
			"boundary": "blueiframe",
		})
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
		capturedAt := time.Now().UTC()
		_ = worker.Hub.Publish(media.Frame{CameraID: worker.CameraID, TankID: worker.TankID, Tier: worker.Tier, MimeType: "image/jpeg", CapturedAt: capturedAt, Data: data})
		if *frames == 1 || *frames%25 == 0 {
			s.updateCameraStatus(worker.CameraID, worker.TankID, "streaming", map[string]any{"mode": "jpeg_worker", "profile": worker.Tier, "target_fps": cameraJPEGTargetFPS, "frames": *frames, "active_viewers": worker.Hub.SubscriberCount(), "reconnect_count": reconnectCount})
		}
	}
}

func workerAlive(worker *cameraJPEGWorker) bool {
	select {
	case <-worker.done:
		return false
	default:
		return true
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
