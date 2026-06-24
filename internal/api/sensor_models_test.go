package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/runtime"
)

// C-13a — POST /v1/sensor-models round-trip + measurement_type validation.
func TestSensorModelsCRUD(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	// 1. POST — 잘못된 measurement_type 거부.
	bad := map[string]any{
		"model_id":         "x_bad",
		"vendor":           "X",
		"product_code":     "Y",
		"display_name":     "Bad",
		"measurement_type": "soil_moisture", // invalid
		"unit":             "%",
	}
	body, _ := json.Marshal(bad)
	r := httptest.NewRequest("POST", "/v1/sensor-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSensorModelsRoute(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid measurement_type: got %d body=%s", w.Code, w.Body.String())
	}

	// 2. POST — 정상.
	good := map[string]any{
		"model_id":                  "ysi_proquatro_ph",
		"vendor":                    "YSI",
		"product_code":              "ProQuatro-pH",
		"display_name":              "YSI ProQuatro pH",
		"measurement_type":          "ph",
		"unit":                      "pH",
		"range_min":                 0.0,
		"range_max":                 14.0,
		"accuracy_value":            0.01,
		"accuracy_unit":             "pH",
		"protocol":                  "rs485",
		"calibration_interval_days": 90,
		"wet_dry":                   "wet_probe",
	}
	body, _ = json.Marshal(good)
	r = httptest.NewRequest("POST", "/v1/sensor-models", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleSensorModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("good POST: got %d body=%s", w.Code, w.Body.String())
	}

	// 3. GET list — 1 개.
	r = httptest.NewRequest("GET", "/v1/sensor-models", nil)
	w = httptest.NewRecorder()
	s.handleSensorModelsRoute(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list GET: got %d", w.Code)
	}
	var list map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list["count"].(float64) != 1 {
		t.Fatalf("expected count=1, got %v", list["count"])
	}

	// 4. DELETE — 자식 인스턴스 없으면 OK.
	r = httptest.NewRequest("DELETE", "/v1/sensor-models/ysi_proquatro_ph", nil)
	w = httptest.NewRecorder()
	s.handleSensorModelItem(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE: got %d body=%s", w.Code, w.Body.String())
	}
}

// C-13a — 자식 인스턴스 있는 모델은 DELETE 시 409 reject.
func TestSensorModelDeleteRejectedWhenInUse(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	// 모델 등록.
	body := []byte(`{"model_id":"m1","vendor":"V","product_code":"P","display_name":"M","measurement_type":"ph","unit":"pH"}`)
	r := httptest.NewRequest("POST", "/v1/sensor-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSensorModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup model POST: %d %s", w.Code, w.Body.String())
	}

	// 인스턴스 등록 (sensor POST 핸들러 경유).
	sensorBody := []byte(`{"sensor_id":"sen_t_01","sensor_type":"water_quality","tank_id":"tank_01","model_id":"m1","mount_location":"mid_depth","measurement_role":["safety_gate_c3"]}`)
	r = httptest.NewRequest("POST", "/v1/sensors", bytes.NewReader(sensorBody))
	w = httptest.NewRecorder()
	s.handleSensorsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup sensor POST: %d %s", w.Code, w.Body.String())
	}

	// 모델 DELETE 시 409 + 메시지.
	r = httptest.NewRequest("DELETE", "/v1/sensor-models/m1", nil)
	w = httptest.NewRecorder()
	s.handleSensorModelItem(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SENSOR_MODEL_IN_USE") {
		t.Fatalf("expected SENSOR_MODEL_IN_USE in body, got: %s", w.Body.String())
	}
}

// C-13a — 잘못된 mount_location enum 은 422 reject.
func TestSensorPostInvalidMountLocation(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	body := []byte(`{"sensor_id":"sen_bad","sensor_type":"water_quality","tank_id":"tank_01","mount_location":"on_the_moon"}`)
	r := httptest.NewRequest("POST", "/v1/sensors", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSensorsRoute(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "INVALID_MOUNT_LOCATION") {
		t.Fatalf("expected INVALID_MOUNT_LOCATION, got: %s", w.Body.String())
	}
}

// C-13a — 잘못된 measurement_role 항목은 422 reject.
func TestSensorPostInvalidMeasurementRole(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	body := []byte(`{"sensor_id":"sen_bad","sensor_type":"water_quality","tank_id":"tank_01","measurement_role":["nuclear_launch"]}`)
	r := httptest.NewRequest("POST", "/v1/sensors", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSensorsRoute(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "INVALID_MEASUREMENT_ROLE") {
		t.Fatalf("expected INVALID_MEASUREMENT_ROLE, got: %s", w.Body.String())
	}
}
