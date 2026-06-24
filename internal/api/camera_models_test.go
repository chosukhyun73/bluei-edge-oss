package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// C-11 — POST /v1/camera-models round-trip + lens_type validation.
func TestCameraModelsCRUD(t *testing.T) {
	s := newTestServer(t)
	// app 가 nil 이면 일부 핸들러가 패닉할 수 있어 최소 구성 만들어둠.
	s.app = runtime.NewApp(s.cfg, s.store)

	// 1. POST — 잘못된 lens_type 거부.
	bad := map[string]any{
		"model_id":     "x_bad",
		"vendor":       "X",
		"product_code": "Y",
		"display_name": "Bad",
		"lens_type":    "stereo", // invalid (must be single/dual/fisheye/ptz/other)
	}
	body, _ := json.Marshal(bad)
	r := httptest.NewRequest("POST", "/v1/camera-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCameraModelsRoute(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid lens_type: got %d, want 422", w.Code)
	}

	// 2. POST — dual 모델 정상 등록.
	dual := map[string]any{
		"model_id":     "stereolabs_zed2i",
		"vendor":       "Stereolabs",
		"product_code": "ZED2i",
		"display_name": "ZED 2i Stereo",
		"lens_type":    "dual",
		"baseline_mm":  60.0,
		"resolution_w": 1920,
		"resolution_h": 1080,
		"fov_deg":      95.0,
		"fps":          30,
		"night_mode":   false,
		"protocols":    []string{"usb", "ethernet"},
	}
	body, _ = json.Marshal(dual)
	r = httptest.NewRequest("POST", "/v1/camera-models", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleCameraModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("dual POST: got %d body=%s", w.Code, w.Body.String())
	}

	// 3. GET list — 1 개.
	r = httptest.NewRequest("GET", "/v1/camera-models", nil)
	w = httptest.NewRecorder()
	s.handleCameraModelsRoute(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list GET: got %d", w.Code)
	}
	var list map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list["count"].(float64) != 1 {
		t.Fatalf("expected count=1, got %v", list["count"])
	}

	// 4. DELETE 모델 — 자식 인스턴스 없으면 OK.
	r = httptest.NewRequest("DELETE", "/v1/camera-models/stereolabs_zed2i", nil)
	w = httptest.NewRecorder()
	s.handleCameraModelItem(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE: got %d body=%s", w.Code, w.Body.String())
	}
}

// C-11 — 자식 인스턴스 있는 모델은 DELETE 시 409 reject.
func TestCameraModelDeleteRejectedWhenInUse(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	body := []byte(`{"model_id":"m1","vendor":"V","product_code":"P","display_name":"M","lens_type":"single"}`)
	r := httptest.NewRequest("POST", "/v1/camera-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCameraModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup model POST: %d %s", w.Code, w.Body.String())
	}

	// 인스턴스 직접 store 로 삽입 (cameras 라우트는 app 의존 — 가볍게 store 우회).
	ctx := r.Context()
	if err := s.store.UpsertCameraProfile(ctx, &storage.CameraProfile{
		CameraID:       "cam_tank_01_01",
		TankID:         "tank_01",
		DisplayName:    "Cam 1",
		Status:         "configured",
		Purpose:        []string{"vision_ai"},
		StreamProfiles: map[string]any{},
		ClipPolicy:     map[string]any{},
		Metadata:       map[string]any{},
		ModelID:        "m1",
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	r = httptest.NewRequest("DELETE", "/v1/camera-models/m1", nil)
	w = httptest.NewRecorder()
	s.handleCameraModelItem(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CAMERA_MODEL_IN_USE") {
		t.Fatalf("expected CAMERA_MODEL_IN_USE in body, got: %s", w.Body.String())
	}
}
