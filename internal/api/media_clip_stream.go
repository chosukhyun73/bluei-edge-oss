package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// handleMediaClipStream serves the mp4 referenced by media.clip.stored events.
// 운영자가 R6.3 trainingPool 에서 받은 baseline clip 을 dispute 라벨링용으로 재생할 때.
//
// GET /v1/media/clips/{clip_id}/play.mp4
//
// vision_observations/{id}/clip.mp4 (R5.1) 과의 차이:
//   - 그쪽: VisionObservation 의 clip_ref (cycle hook 캡처)
//   - 이쪽: MediaClipStoredPayload 의 URI (cycle hook + 상시 캡처 모두)
//
// 보안: clip_id 로 events 조회 후 URI 사용. 단 URI 도 capture.DefaultTempDir prefix 검증.
func (s *Server) handleMediaClipStream(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/v1/media/clips/")
	rel = strings.TrimSuffix(rel, "/")
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/media/clips/{clip_id}/{play.mp4|exclude}", "")
		return
	}
	clipID := parts[0]
	// R8: clip 격리 액션
	if parts[1] == "exclude" {
		s.handleMediaClipExclude(w, r, clipID)
		return
	}
	if parts[1] != "play.mp4" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected play.mp4 or exclude action", "")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// media.clip.stored events 에서 clip_id 매칭.
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventMediaClipStored,
		Limit:     500,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var uri string
	for _, e := range eventsList {
		var payload events.MediaClipStoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload.ClipID == clipID {
			uri = payload.URI
			break
		}
	}
	if uri == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"media clip not found for clip_id", "")
		return
	}

	cleaned := filepath.Clean(uri)
	if !filepath.IsAbs(cleaned) {
		writeError(w, http.StatusForbidden, "INVALID_URI", "uri must be absolute", "")
		return
	}
	captureDir := filepath.Clean(capture.DefaultTempDir)
	if !strings.HasPrefix(cleaned, captureDir+string(filepath.Separator)) && cleaned != captureDir {
		writeError(w, http.StatusForbidden, "OUTSIDE_CAPTURES",
			"uri must reside under "+captureDir, "")
		return
	}
	fi, statErr := os.Stat(cleaned)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			writeError(w, http.StatusNotFound, "FILE_NOT_FOUND",
				"clip mp4 no longer exists (retention rotated?)", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "STAT_FAILED", statErr.Error(), "")
		return
	}
	if fi.IsDir() {
		writeError(w, http.StatusForbidden, "INVALID_URI", "uri is a directory", "")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, cleaned)
}
