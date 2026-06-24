package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type cameraPreviewProcess struct {
	CameraID string
	Dir      string
	Cmd      *exec.Cmd
	Started  time.Time
}

var cameraPreviewRegistry = struct {
	sync.Mutex
	items map[string]*cameraPreviewProcess
}{items: map[string]*cameraPreviewProcess{}}

func (s *Server) handleCameraPreviewStart(w http.ResponseWriter, r *http.Request, cameraID string) {
	profile, err := s.store.GetCameraProfile(r.Context(), cameraID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "camera profile not found", "")
		return
	}
	rtspURL, err := s.cameraRTSPURL(r.Context(), profile, r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "CAMERA_URL_ERROR", err.Error(), "")
		return
	}
	proc, err := startCameraHLSPreview(cameraID, rtspURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CAMERA_PREVIEW_START_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"camera_id":    cameraID,
		"playlist_url": fmt.Sprintf("/v1/cameras/%s/preview/index.m3u8", cameraID),
		"started_at":   proc.Started.Format(time.RFC3339Nano),
	})
}

func (s *Server) handleCameraPreviewStop(w http.ResponseWriter, r *http.Request, cameraID string) {
	stopped := stopCameraHLSPreview(cameraID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "camera_id": cameraID, "stopped": stopped})
}

func (s *Server) handleCameraPreviewFile(w http.ResponseWriter, r *http.Request, cameraID, name string) {
	cameraPreviewRegistry.Lock()
	proc := cameraPreviewRegistry.items[cameraID]
	cameraPreviewRegistry.Unlock()
	if proc == nil {
		writeError(w, http.StatusNotFound, "PREVIEW_NOT_RUNNING", "camera preview is not running", "")
		return
	}
	path := filepath.Join(proc.Dir, filepath.Base(name))
	if name == "index.m3u8" {
		data, err := os.ReadFile(path)
		if err != nil {
			writeError(w, http.StatusNotFound, "PLAYLIST_NOT_READY", "preview playlist is not ready yet", "")
			return
		}
		base := fmt.Sprintf("/v1/cameras/%s/preview/", cameraID)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.HasSuffix(line, ".ts") && !strings.HasPrefix(line, "/") {
				lines[i] = base + line
			}
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(strings.Join(lines, "\n")))
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

func startCameraHLSPreview(cameraID, rtspURL string) (*cameraPreviewProcess, error) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return nil, fmt.Errorf("gst-launch-1.0 is not installed")
	}
	cameraPreviewRegistry.Lock()
	if old := cameraPreviewRegistry.items[cameraID]; old != nil {
		_ = old.Cmd.Process.Kill()
		_ = os.RemoveAll(old.Dir)
		delete(cameraPreviewRegistry.items, cameraID)
	}
	cameraPreviewRegistry.Unlock()
	dir, err := os.MkdirTemp("", "bluei-camera-hls-*")
	if err != nil {
		return nil, err
	}
	playlist := filepath.Join(dir, "index.m3u8")
	segments := filepath.Join(dir, "segment%05d.ts")
	cmd := exec.CommandContext(context.Background(), "gst-launch-1.0", "-q",
		"rtspsrc", "location="+rtspURL, "latency=800", "protocols=tcp",
		"!", "rtph264depay",
		"!", "h264parse", "config-interval=-1",
		"!", "mpegtsmux",
		"!", "hlssink", "max-files=90", "target-duration=2", "playlist-length=30", "location="+segments, "playlist-location="+playlist)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	proc := &cameraPreviewProcess{CameraID: cameraID, Dir: dir, Cmd: cmd, Started: time.Now().UTC()}
	cameraPreviewRegistry.Lock()
	cameraPreviewRegistry.items[cameraID] = proc
	cameraPreviewRegistry.Unlock()
	go func() {
		_ = cmd.Wait()
		cameraPreviewRegistry.Lock()
		if cameraPreviewRegistry.items[cameraID] == proc {
			delete(cameraPreviewRegistry.items, cameraID)
			_ = os.RemoveAll(dir)
		}
		cameraPreviewRegistry.Unlock()
	}()
	return proc, nil
}

func stopCameraHLSPreview(cameraID string) bool {
	cameraPreviewRegistry.Lock()
	proc := cameraPreviewRegistry.items[cameraID]
	if proc != nil {
		delete(cameraPreviewRegistry.items, cameraID)
	}
	cameraPreviewRegistry.Unlock()
	if proc == nil {
		return false
	}
	_ = proc.Cmd.Process.Kill()
	_ = os.RemoveAll(proc.Dir)
	return true
}
