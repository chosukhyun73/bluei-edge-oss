package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
// captureDirAllowed reports whether cleaned (이미 filepath.Clean 된 절대경로) 이
// 허용된 captures 디렉토리 하위인지 검사한다(path traversal 방어).
// 허용: 설정 temp_dir(상대→절대 정규화) + legacy /tmp DefaultTempDir(기존 클립 전환 호환).
func (s *Server) captureDirAllowed(cleaned string) bool {
	dirs := []string{filepath.Clean(capture.DefaultTempDir)}
	if ad := s.cfg.Capture.ArchiveDir; ad != "" {
		if abs, err := filepath.Abs(ad); err == nil {
			dirs = append(dirs, filepath.Clean(abs))
		}
	}
	if td := s.cfg.Capture.TempDir; td != "" {
		if abs, err := filepath.Abs(td); err == nil {
			dirs = append(dirs, filepath.Clean(abs))
		} else {
			dirs = append(dirs, filepath.Clean(td))
		}
	}
	for _, d := range dirs {
		if cleaned == d || strings.HasPrefix(cleaned, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolveClipPath — 클립 실파일 경로. 로컬(TempDir 기준 uri)에 없으면 ArchiveDir 에서
// 같은 파일명을 찾는다. 오래된 클립을 NAS/외장으로 옮긴 뒤에도 재생·라벨링 목록·학습
// 매니페스트가 그대로 동작하게 하는 폴백이다 (2026-09-18).
// 반환: 실제로 존재하는 경로와 존재 여부. 둘 다 없으면 원본 uri 와 false.
func (s *Server) resolveClipPath(uri string) (string, bool) {
	if uri == "" {
		return "", false
	}
	if _, err := os.Stat(uri); err == nil {
		return uri, true
	}
	ad := s.cfg.Capture.ArchiveDir
	if ad == "" {
		return uri, false
	}
	alt := filepath.Join(ad, filepath.Base(uri))
	if _, err := os.Stat(alt); err == nil {
		return alt, true
	}
	return uri, false
}

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
	if parts[1] != "play.mp4" && parts[1] != "poster.jpg" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected play.mp4, poster.jpg or exclude action", "")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// media.clip.stored events 에서 clip_id 매칭 — DB 단 clip_id 필터로 직접 조회
	// (상시 baseline 이 급이 클립을 최근 N건 윈도우 밖으로 밀어내 404 나던 문제 방지).
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:     events.EventMediaClipStored,
		PayloadClipID: clipID,
		Limit:         1,
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
	if !s.captureDirAllowed(cleaned) {
		writeError(w, http.StatusForbidden, "OUTSIDE_CAPTURES",
			"uri must reside under a configured captures dir", "")
		return
	}
	// poster.jpg 액션이면 sibling <clip>.jpg (박스그리기 저장프레임) 를 서빙.
	// ?offset=N(초) 지정 시 mp4 의 해당 시점 프레임을 즉석 추출해 서빙 — 포스터
	// (녹화 시작 고정 1장)만으로는 라벨링 장면 다양성이 안 나옴 (2026-07-17).
	// 추출 실패 시 정적 포스터로 폴백.
	servePath, _ := s.resolveClipPath(cleaned) // 로컬에 없으면 아카이브(NAS/외장)
	contentType := "video/mp4"
	if parts[1] == "poster.jpg" {
		if off, err := strconv.ParseFloat(r.URL.Query().Get("offset"), 64); err == nil && off > 0 {
			if jpg, xerr := extractClipFrame(r.Context(), servePath, off); xerr == nil {
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Cache-Control", "private, max-age=3600")
				_, _ = w.Write(jpg)
				return
			} else {
				slog.Warn("clip frame extract failed — falling back to poster",
					"clip_id", clipID, "offset", off, "error", xerr)
			}
		}
		servePath = strings.TrimSuffix(servePath, ".mp4") + ".jpg"
		contentType = "image/jpeg"
	}
	fi, statErr := os.Stat(servePath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			writeError(w, http.StatusNotFound, "FILE_NOT_FOUND",
				"clip file no longer exists (retention rotated?)", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "STAT_FAILED", statErr.Error(), "")
		return
	}
	if fi.IsDir() {
		writeError(w, http.StatusForbidden, "INVALID_URI", "uri is a directory", "")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, servePath)
}

// clipFramePython — 프레임 추출 스크립트 인터프리터. av 모듈이 있는 venv 를
// 우선하고(clip 디코드에 필요, pulse_discharge_check 와 동일 제약), 없으면
// 시스템 python3 (실패 시 정적 포스터 폴백이 흡수).
func clipFramePython() string {
	venv := "local-ai/training/.venv-convert/bin/python"
	if _, err := os.Stat(venv); err == nil {
		return venv
	}
	return "python3"
}

// extractClipFrame — mp4 의 offset(초) 시점 프레임 1장을 JPEG 바이트로 추출.
// scripts/clip_frame.py (PyAV) 실행. 상한 10s 타임아웃.
func extractClipFrame(ctx context.Context, mp4Path string, offsetSec float64) ([]byte, error) {
	if offsetSec < 0 || offsetSec > 6*3600 {
		return nil, fmt.Errorf("offset out of range: %v", offsetSec)
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, clipFramePython(), "scripts/clip_frame.py",
		mp4Path, strconv.FormatFloat(offsetSec, 'f', 1, 64)).Output()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty frame output")
	}
	return out, nil
}
