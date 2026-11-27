package api

import (
	"net/http"
)

// handleCaptureStatus — GET /v1/capture/status. 상시 캡처 동작 상태 + 설정 요약.
func (s *Server) handleCaptureStatus(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Capture
	running := false
	if s.capture != nil {
		running = s.capture.IsRunning()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":        s.capture != nil,
		"running":          running,
		"enabled":          c.Enabled,
		"mode":             c.Mode, // fixture | rtsp
		"tanks":            c.ContinuousTanks,
		"duration_seconds": c.DurationSeconds,
		"spacing_seconds":  c.ContinuousModeSpacing,
		"temp_dir":         c.TempDir,
	})
}

// handleCaptureStart — POST /v1/capture/start. 상시 캡처(학습 데이터 수집) 시작.
func (s *Server) handleCaptureStart(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		writeError(w, http.StatusServiceUnavailable, "CAPTURE_UNAVAILABLE", "capture worker not wired", "")
		return
	}
	if err := s.capture.StartContinuous(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "CAPTURE_START_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "running": true})
}

// handleCaptureStop — POST /v1/capture/stop. 상시 캡처 중지.
func (s *Server) handleCaptureStop(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		writeError(w, http.StatusServiceUnavailable, "CAPTURE_UNAVAILABLE", "capture worker not wired", "")
		return
	}
	s.capture.StopContinuous()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "running": false})
}
