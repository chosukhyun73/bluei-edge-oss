package api

import (
	"net/http"
	"os/exec"
	"time"
)

func (s *Server) handleCameraMJPEG(w http.ResponseWriter, r *http.Request, cameraID string) {
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
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		writeError(w, http.StatusInternalServerError, "CAMERA_RUNTIME_MISSING", "gst-launch-1.0 is not installed", "")
		return
	}
	// MJPEG is a long-lived response. The API server has a short global
	// WriteTimeout for normal JSON requests; clear the per-response write
	// deadline here so the browser is not disconnected every request_timeout_sec.
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}

	tier := r.URL.Query().Get("profile")
	if tier == "" {
		tier = "sub"
	}
	worker, err := s.ensureCameraJPEGWorker(r.Context(), profile, tier, rtspURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CAMERA_LIVE_FAILED", err.Error(), "")
		return
	}
	if !cameraStreamLimits.AllowViewer(worker.Hub.SubscriberCount()) {
		writeError(w, http.StatusTooManyRequests, "CAMERA_VIEWER_LIMIT", "camera viewer limit reached", "")
		return
	}
	frames, cancel := worker.Hub.Subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=blueiframe")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	if latest, ok := worker.Hub.Latest(); ok {
		if err := writeMultipartJPEG(w, latest); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}
			if err := writeMultipartJPEG(w, frame); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}
