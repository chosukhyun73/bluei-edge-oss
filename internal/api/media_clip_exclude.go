package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type mediaClipExcludeRequest struct {
	Reason     string `json:"reason"` // occlusion | low_visibility | other
	Memo       string `json:"memo,omitempty"`
	OperatorID string `json:"operator_id,omitempty"`
}

// handleMediaClipExclude — R8. 학습 데이터로 쓸 수 없는 영상 제외/보류 → 즉시 삭제.
//
// POST /v1/media/clips/{clip_id}/exclude
//
// 동작:
//  1. media.clip.stored events 에서 clip_id 매칭 → 원본 URI
//  2. URI 가 captures dir 안에 있는지 검증 (path traversal 방지)
//  3. 원본 mp4 + poster(.jpg) 즉시 삭제 (reason=unsure 보류 포함)
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
	switch req.Reason {
	case "occlusion", "low_visibility", "other", "unsure":
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_REASON",
			"reason must be 'occlusion', 'low_visibility', 'other', or 'unsure'", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}

	// 1. clip_id 로 원본 URI 조회 — DB 단 clip_id 필터로 직접 조회
	// (급이 클립이 baseline 에 파묻혀 조회 실패하던 문제 방지).
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:     events.EventMediaClipStored,
		PayloadClipID: clipID,
		Limit:         1,
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
	if !s.captureDirAllowed(cleaned) {
		writeError(w, http.StatusForbidden, "OUTSIDE_CAPTURES",
			"clip uri must reside under a configured captures dir", "")
		return
	}

	// 3. 원본 mp4 + poster(.jpg) 즉시 삭제 (보류/제외 = 삭제 정책). 파일이 이미 없어도 event 는 적재.
	newURI := "" // 삭제 → 이동 위치 없음
	if _, statErr := os.Stat(cleaned); statErr == nil {
		if err := os.Remove(cleaned); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED",
				"파일 삭제 실패: "+err.Error(), "")
			return
		}
	}
	_ = os.Remove(strings.TrimSuffix(cleaned, ".mp4") + ".jpg")

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
