package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type mediaClipExcludeRequest struct {
	Reason     string `json:"reason"` // occlusion | low_visibility | other
	Memo       string `json:"memo,omitempty"`
	OperatorID string `json:"operator_id,omitempty"`
}

// handleMediaClipExclude — R8. 학습 데이터로 쓸 수 없는 영상 격리.
//
// POST /v1/media/clips/{clip_id}/exclude
//
// 동작:
//  1. media.clip.stored events 에서 clip_id 매칭 → 원본 URI
//  2. URI 가 captures dir 안에 있는지 검증 (path traversal 방지)
//  3. 파일을 captures/excluded/<reason>/<basename> 으로 mv
//  4. media.clip.excluded event 적재
//
// training-pool API 가 excluded events 로 자동 필터링.
func (s *Server) handleMediaClipExclude(w http.ResponseWriter, r *http.Request, clipID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req mediaClipExcludeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.Reason != "occlusion" && req.Reason != "low_visibility" && req.Reason != "other" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_REASON",
			"reason must be 'occlusion', 'low_visibility', or 'other'", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}

	// 1. clip_id 로 원본 URI 조회
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventMediaClipStored,
		Limit:     500,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var origURI string
	for _, e := range eventsList {
		var payload events.MediaClipStoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload.ClipID == clipID {
			origURI = payload.URI
			break
		}
	}
	if origURI == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"media clip not found for clip_id", "")
		return
	}

	// 2. path traversal 검증
	cleaned := filepath.Clean(origURI)
	if !filepath.IsAbs(cleaned) {
		writeError(w, http.StatusForbidden, "INVALID_URI", "uri must be absolute", "")
		return
	}
	captureDir := filepath.Clean(capture.DefaultTempDir)
	if !strings.HasPrefix(cleaned, captureDir+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "OUTSIDE_CAPTURES",
			"clip uri must reside under "+captureDir, "")
		return
	}

	// 3. excluded/<reason>/ 디렉토리 + mv
	excludedDir := filepath.Join(captureDir, "excluded", req.Reason)
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "MKDIR_FAILED", err.Error(), "")
		return
	}
	newURI := filepath.Join(excludedDir, filepath.Base(cleaned))
	if _, statErr := os.Stat(cleaned); os.IsNotExist(statErr) {
		// 파일이 이미 없음 (retention 자동 삭제됨 등) — event 만 적재하고 진행.
		newURI = ""
	} else if err := os.Rename(cleaned, newURI); err != nil {
		writeError(w, http.StatusInternalServerError, "MOVE_FAILED",
			"파일 이동 실패: "+err.Error(), "")
		return
	}

	// 4. event 적재
	payload := events.MediaClipExcludedPayload{
		ClipID:      clipID,
		Reason:      req.Reason,
		Memo:        req.Memo,
		OperatorID:  req.OperatorID,
		OriginalURI: origURI,
		NewURI:      newURI,
		ExcludedAt:  common.FormatTime(common.NowUTC()),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_EXCLUDED_PAYLOAD", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "operator", clipID,
		events.EventMediaClipExcluded, clipID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"sequence": seq,
		"excluded": payload,
	})
}
