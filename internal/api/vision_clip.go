package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bluei.kr/edge/internal/capture"
)

// handleVisionObservationClip serves the 7-second mp4 that was captured at
// the time of this observation. Used by R5 DisputeTab so the operator can
// replay the exact clip that LRCN scored, not "the camera's current snapshot".
//
// Security:
//   - clip_ref is read from events (server-authored, but defensive Clean+prefix
//     check anyway in case future writers leak external paths).
//   - only files under capture.DefaultTempDir are served.
//
// Range header is forwarded to http.ServeFile which supports it natively.
func (s *Server) handleVisionObservationClip(w http.ResponseWriter, r *http.Request, observationID string) {
	observation, err := s.findVisionObservation(r, observationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if observation == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "vision observation not found", "")
		return
	}
	ref := observation.Payload.ClipRef
	if ref == "" {
		writeError(w, http.StatusNotFound, "NO_CLIP", "this observation has no clip_ref", "")
		return
	}

	// Path traversal 방지. clip_ref 는 capture worker 가 적재한 path 라
	// 신뢰 가능하지만, 미래의 다른 writer 가 외부 path 를 박을 가능성 차단.
	cleaned := filepath.Clean(ref)
	if !filepath.IsAbs(cleaned) {
		writeError(w, http.StatusForbidden, "INVALID_CLIP_PATH", "clip_ref must be absolute", "")
		return
	}
	captureDir := filepath.Clean(capture.DefaultTempDir)
	// Require captureDir + separator prefix so "/tmp/bluei-edge/capturesX/..." 같은
	// substring 우회 차단.
	if !strings.HasPrefix(cleaned, captureDir+string(filepath.Separator)) && cleaned != captureDir {
		writeError(w, http.StatusForbidden, "OUTSIDE_CAPTURES",
			"clip_ref must reside under "+captureDir, "")
		return
	}

	fi, statErr := os.Stat(cleaned)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			writeError(w, http.StatusNotFound, "CLIP_FILE_NOT_FOUND",
				"clip mp4 file no longer exists (retention rotated?)", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "STAT_FAILED", statErr.Error(), "")
		return
	}
	if fi.IsDir() {
		writeError(w, http.StatusForbidden, "INVALID_CLIP_PATH", "clip_ref is a directory", "")
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, max-age=300")
	// ServeFile 이 Range 헤더 + Last-Modified + ETag 자동 처리.
	http.ServeFile(w, r, cleaned)
}
