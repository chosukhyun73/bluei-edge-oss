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
	"bluei.kr/edge/internal/vision_pipeline"
)

// 바닥 상태 스틸 목록/이미지/라벨 (yolo-lrcn-fusion 설계 3.2 — 박스 라벨로 통일 2026-07-08).
//
//	GET  /v1/vision/floor-stills?limit=&tank_id=&unlabeled=1 — 스틸 + 박스 라벨 여부 join
//	GET  /v1/vision/floor-stills/image?still_id=             — JPEG 서빙
//	POST /v1/vision/floor-stills/label                       — (구) 4클래스 라벨. UI 사용 중단, 계약 잔존.
//
// 라벨은 bootstrap 박스 계약(vision.bootstrap.label.recorded + origin_still_id)으로 저장되고,
// 이 목록은 origin_still_id join 으로 "박스 라벨 완료" 여부만 노출한다.

// floorStillStoredPayload — media.floor_still.stored 파싱용 (vision_pipeline 이 적재).
type floorStillStoredPayload struct {
	StillID    string `json:"still_id"`
	TankID     string `json:"tank_id"`
	CameraID   string `json:"camera_id"`
	Path       string `json:"path"`
	Trigger    string `json:"trigger"`
	CycleID    string `json:"cycle_id"`
	CapturedAt string `json:"captured_at"`
}

// floorStillScanLimit — 스틸 이벤트 스캔 윈도우. 하루 ~50장 기준 2주치 이상.
const floorStillScanLimit = 2000

func (s *Server) queryFloorStills(r *http.Request) (map[string]*floorStillStoredPayload, []string, error) {
	evs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: vision_pipeline.EventFloorStillStored, Limit: floorStillScanLimit,
	})
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]*floorStillStoredPayload{}
	var order []string // 최신순 (QueryEvents 는 seq DESC)
	for _, e := range evs {
		var p floorStillStoredPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.StillID == "" {
			continue
		}
		if _, seen := byID[p.StillID]; seen {
			continue
		}
		byID[p.StillID] = &p
		order = append(order, p.StillID)
	}
	return byID, order, nil
}

func (s *Server) handleVisionFloorStills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := intParam(r, "limit", 100, 500)
	tankID := r.URL.Query().Get("tank_id")
	unlabeledOnly := r.URL.Query().Get("unlabeled") == "1"

	byID, order, err := s.queryFloorStills(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 박스 라벨 완료 join — bootstrap 이벤트의 origin_still_id 기준.
	labelEvs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionBootstrapLabelRecorded, Limit: floorStillScanLimit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	boxLabeled := map[string]bool{}
	for _, e := range labelEvs {
		var p events.VisionBootstrapLabelRecordedPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.OriginStillID == "" {
			continue
		}
		boxLabeled[p.OriginStillID] = true
	}

	items := []map[string]any{}
	labeledCount := 0
	totalStills := 0
	for _, id := range order {
		p := byID[id]
		if tankID != "" && p.TankID != tankID {
			continue
		}
		// 백업 이동/삭제된 파일은 목록에서 제외 (fusion 설계 3.1 — 존재 확인으로 필터).
		if fi, serr := os.Stat(p.Path); serr != nil || fi.IsDir() {
			continue
		}
		totalStills++
		labeled := boxLabeled[id]
		if labeled {
			labeledCount++
		}
		if unlabeledOnly && labeled {
			continue
		}
		if len(items) >= limit {
			continue // labeledCount 집계는 계속
		}
		items = append(items, map[string]any{
			"still_id":    p.StillID,
			"tank_id":     p.TankID,
			"camera_id":   p.CameraID,
			"trigger":     p.Trigger,
			"cycle_id":    p.CycleID,
			"captured_at": p.CapturedAt,
			"box_labeled": labeled,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         items,
		"count":         len(items),
		"total_stills":  totalStills,
		"total_labeled": labeledCount,
	})
}

func (s *Server) handleVisionFloorStillImage(w http.ResponseWriter, r *http.Request) {
	stillID := r.URL.Query().Get("still_id")
	if stillID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_STILL_ID", "still_id query param required", "")
		return
	}
	byID, _, err := s.queryFloorStills(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	p := byID[stillID]
	if p == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "floor still not found", "")
		return
	}
	// path 는 서버가 적재한 값이지만 방어적으로 floor_stills 루트 밖 접근 차단
	// (vision_clip.go 와 동일 정책).
	cleaned := filepath.Clean(p.Path)
	root := filepath.Clean(filepath.Join(s.cfg.Edge.DataDir, "floor_stills"))
	absRoot, aerr := filepath.Abs(root)
	absPath, perr := filepath.Abs(cleaned)
	if aerr != nil || perr != nil || !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "OUTSIDE_FLOOR_STILLS",
			"still path must reside under "+root, "")
		return
	}
	if fi, serr := os.Stat(absPath); serr != nil || fi.IsDir() {
		writeError(w, http.StatusNotFound, "STILL_FILE_NOT_FOUND", "still file no longer exists", "")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, absPath)
}

type floorStillLabelRequest struct {
	StillID    string `json:"still_id"`
	Class      string `json:"class"`
	OperatorID string `json:"operator_id"`
}

func (s *Server) handleVisionFloorStillLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req floorStillLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	byID, _, err := s.queryFloorStills(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	still := byID[req.StillID]
	if still == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "floor still not found: "+req.StillID, "")
		return
	}
	payload := events.FloorStillLabelPayload{
		LabelID:    common.NewID("floor_label"),
		StillID:    still.StillID,
		TankID:     still.TankID,
		CameraID:   still.CameraID,
		OperatorID: req.OperatorID,
		Class:      req.Class,
		LabeledAt:  common.FormatTime(common.NowUTC()),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_FLOOR_LABEL", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "vision", payload.CameraID,
		events.EventFloorStillLabelRecorded, payload.LabelID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sequence": seq, "label": payload})
}
