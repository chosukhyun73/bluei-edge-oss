package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/vision_pipeline"
)

func seedFloorStill(t *testing.T, s *Server, stillID, tankID, path string) {
	t.Helper()
	_, err := s.app.AppendEvent(context.Background(), "g3_pipeline", "vision", "cam_underwater_7_1",
		vision_pipeline.EventFloorStillStored, stillID, map[string]any{
			"still_id": stillID, "tank_id": tankID, "camera_id": "cam_underwater_7_1",
			"path": path, "trigger": "periodic", "cycle_id": "",
			"captured_at": "2026-07-06T12:00:00Z",
		})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFloorStillListLabelImage(t *testing.T) {
	s := newTestServerWithApp(t)
	s.cfg.Edge.DataDir = t.TempDir()

	// 스틸 파일 + stored 이벤트 seed.
	tankDir := filepath.Join(s.cfg.Edge.DataDir, "floor_stills", "tank_01")
	if err := os.MkdirAll(tankDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stillPath := filepath.Join(tankDir, "20260706T120000_periodic.jpg")
	if err := os.WriteFile(stillPath, []byte("jpegdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedFloorStill(t, s, "tank_01_20260706T120000_periodic", "tank_01", stillPath)

	// 목록 — 박스 라벨 없음.
	w := httptest.NewRecorder()
	s.handleVisionFloorStills(w, httptest.NewRequest(http.MethodGet, "/v1/vision/floor-stills", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list code=%d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Items        []map[string]any `json:"items"`
		TotalStills  int              `json:"total_stills"`
		TotalLabeled int              `json:"total_labeled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.TotalLabeled != 0 {
		t.Fatalf("list = %+v", list)
	}
	if list.Items[0]["box_labeled"] != false {
		t.Fatalf("unlabeled item box_labeled = %v", list.Items[0]["box_labeled"])
	}

	// (구) 4클래스 라벨 API — 계약 잔존 확인 (UI 사용 중단, 목록에는 반영 안 됨).
	body, _ := json.Marshal(map[string]string{
		"still_id": "tank_01_20260706T120000_periodic", "class": "pellet", "operator_id": "op_cho",
	})
	w = httptest.NewRecorder()
	s.handleVisionFloorStillLabel(w, httptest.NewRequest(http.MethodPost, "/v1/vision/floor-stills/label", bytes.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("label code=%d body=%s", w.Code, w.Body.String())
	}

	// 박스 라벨 (bootstrap + origin_still_id) → 목록 join + unlabeled 필터.
	body, _ = json.Marshal(map[string]any{
		"camera_id": "cam_underwater_7_1", "tank_id": "tank_01",
		"snapshot_ref":    "tank_01_20260706T120000_periodic",
		"origin_still_id": "tank_01_20260706T120000_periodic",
		"operator_id":     "op_cho",
		"boxes": []map[string]any{
			{"x": 0.1, "y": 0.1, "w": 0.2, "h": 0.2, "class": "pellet"},
		},
	})
	w = httptest.NewRecorder()
	s.handlePostBootstrapLabel(w, httptest.NewRequest(http.MethodPost, "/v1/vision/bootstrap/labels", bytes.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap label code=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleVisionFloorStills(w, httptest.NewRequest(http.MethodGet, "/v1/vision/floor-stills", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.TotalLabeled != 1 || list.Items[0]["box_labeled"] != true {
		t.Fatalf("labeled list = %+v", list)
	}
	w = httptest.NewRecorder()
	s.handleVisionFloorStills(w, httptest.NewRequest(http.MethodGet, "/v1/vision/floor-stills?unlabeled=1", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("unlabeled filter must hide labeled still: %+v", list.Items)
	}

	// 파일 이동/삭제된 스틸은 목록에서 제외 (백업 이동 지원).
	if err := os.Remove(stillPath); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.handleVisionFloorStills(w, httptest.NewRequest(http.MethodGet, "/v1/vision/floor-stills", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.TotalStills != 0 || len(list.Items) != 0 {
		t.Fatalf("moved still must disappear from list: %+v", list)
	}
	if err := os.WriteFile(stillPath, []byte("jpegdata"), 0o644); err != nil {
		t.Fatal(err) // 이후 이미지 서빙 검증 위해 복원
	}

	// 이미지 서빙.
	w = httptest.NewRecorder()
	s.handleVisionFloorStillImage(w, httptest.NewRequest(http.MethodGet,
		"/v1/vision/floor-stills/image?still_id=tank_01_20260706T120000_periodic", nil))
	if w.Code != http.StatusOK || w.Body.String() != "jpegdata" {
		t.Fatalf("image code=%d len=%d", w.Code, w.Body.Len())
	}

	// 루트 밖 path 차단.
	outside := filepath.Join(t.TempDir(), "evil.jpg")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	seedFloorStill(t, s, "evil_still", "tank_01", outside)
	w = httptest.NewRecorder()
	s.handleVisionFloorStillImage(w, httptest.NewRequest(http.MethodGet,
		"/v1/vision/floor-stills/image?still_id=evil_still", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("outside path must be forbidden, code=%d", w.Code)
	}
	_ = events.FloorStillClasses // 계약 참조 유지
}
