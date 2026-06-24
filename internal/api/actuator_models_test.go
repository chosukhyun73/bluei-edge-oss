package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/runtime"
)

// C-13b — POST /v1/actuator-models round-trip + device_category validation.
func TestActuatorModelsCRUD(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	// 1. POST — 잘못된 device_category 거부.
	bad := map[string]any{
		"model_id":        "x_bad",
		"vendor":          "X",
		"product_code":    "Y",
		"display_name":    "Bad",
		"device_category": "unknown_thing",
	}
	body, _ := json.Marshal(bad)
	r := httptest.NewRequest("POST", "/v1/actuator-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleActuatorModelsRoute(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid device_category: got %d, want 422", w.Code)
	}

	// 2. POST — pump 모델 정상 등록.
	pump := map[string]any{
		"model_id":                    "grundfos_cre_5_1",
		"vendor":                      "Grundfos",
		"product_code":                "CRE-5-1",
		"display_name":                "Grundfos CRE 5-1 순환 펌프",
		"device_category":             "pump",
		"rated_power_w":               1500.0,
		"capacity_value":              20.0,
		"capacity_unit":               "m3/h",
		"control_method":              "modbus",
		"response_time_s":             2.0,
		"consumable_replacement_days": 365,
		"notes":                       "RAS 순환 펌프",
	}
	body, _ = json.Marshal(pump)
	r = httptest.NewRequest("POST", "/v1/actuator-models", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleActuatorModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("pump POST: got %d body=%s", w.Code, w.Body.String())
	}

	// 3. GET list — 1 개.
	r = httptest.NewRequest("GET", "/v1/actuator-models", nil)
	w = httptest.NewRecorder()
	s.handleActuatorModelsRoute(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list GET: got %d", w.Code)
	}
	var list map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list["count"].(float64) != 1 {
		t.Fatalf("expected count=1, got %v", list["count"])
	}

	// 4. POST — 잘못된 control_method 거부.
	badCtrl := map[string]any{
		"model_id":        "test_bad_ctrl",
		"vendor":          "T",
		"product_code":    "T1",
		"display_name":    "T",
		"device_category": "pump",
		"control_method":  "telepathy",
	}
	body, _ = json.Marshal(badCtrl)
	r = httptest.NewRequest("POST", "/v1/actuator-models", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleActuatorModelsRoute(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid control_method: got %d want 422", w.Code)
	}

	// 5. DELETE 모델 — 자식 인스턴스 없으면 OK.
	r = httptest.NewRequest("DELETE", "/v1/actuator-models/grundfos_cre_5_1", nil)
	w = httptest.NewRecorder()
	s.handleActuatorModelItem(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE: got %d body=%s", w.Code, w.Body.String())
	}
}

// C-13b — 카테고리별 category_specs 필수 키 검증 + round-trip.
func TestActuatorModelCategorySpecs(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	post := func(body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/v1/actuator-models", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.handleActuatorModelsRoute(w, r)
		return w
	}

	// 1. circulation_pump — 필수 키(max_flow_m3h) 누락 시 거부.
	w := post(map[string]any{
		"model_id": "wilo_bad", "vendor": "Wilo", "product_code": "PU-S600U",
		"display_name": "Wilo 순환펌프", "device_category": "circulation_pump",
		"category_specs": map[string]any{"max_head_m": 15.3},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("circulation_pump missing spec: got %d want 422 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "max_flow_m3h") {
		t.Fatalf("expected missing-key msg, got: %s", w.Body.String())
	}

	// 2. circulation_pump — 필수 키 모두 있으면 등록 + round-trip.
	w = post(map[string]any{
		"model_id": "wilo_pu_s600u", "vendor": "Wilo", "product_code": "PU-S600U",
		"display_name": "Wilo 순환펌프", "device_category": "circulation_pump",
		"category_specs": map[string]any{"max_head_m": 15.3, "max_flow_m3h": 11.4, "rated_current_a": 1.6},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("circulation_pump valid: got %d body=%s", w.Code, w.Body.String())
	}
	got, err := s.store.GetActuatorModel(context.Background(), "wilo_pu_s600u")
	if err != nil || got == nil {
		t.Fatalf("GetActuatorModel: %v %v", got, err)
	}
	var specs map[string]any
	if err := json.Unmarshal([]byte(got.CategorySpecs), &specs); err != nil {
		t.Fatalf("category_specs not JSON: %q err=%v", got.CategorySpecs, err)
	}
	if specs["max_flow_m3h"].(float64) != 11.4 {
		t.Fatalf("category_specs round-trip: %+v", specs)
	}

	// 3. air_pump — air_flow 단위 둘 다 없으면 거부.
	w = post(map[string]any{
		"model_id": "blower_bad", "vendor": "X", "product_code": "B1",
		"display_name": "Blower", "device_category": "air_pump",
		"category_specs": map[string]any{"air_pressure_kpa": 20},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("air_pump missing air_flow: got %d body=%s", w.Code, w.Body.String())
	}

	// 4. air_pump — lpm 단위로도 통과.
	w = post(map[string]any{
		"model_id": "blower_ok", "vendor": "X", "product_code": "B1",
		"display_name": "Blower", "device_category": "air_pump",
		"category_specs": map[string]any{"air_flow_lpm": 80, "air_pressure_kpa": 20},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("air_pump lpm: got %d body=%s", w.Code, w.Body.String())
	}

	// 5. 기존 pump — category_specs 없어도 통과 (하위호환).
	w = post(map[string]any{
		"model_id": "legacy_pump", "vendor": "G", "product_code": "P",
		"display_name": "Legacy", "device_category": "pump",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("legacy pump no specs: got %d body=%s", w.Code, w.Body.String())
	}
}

// C-13b — 자식 인스턴스 있는 모델은 DELETE 시 409 reject.
func TestActuatorModelDeleteRejectedWhenInUse(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	body := []byte(`{"model_id":"m1","vendor":"V","product_code":"P","display_name":"M","device_category":"pump"}`)
	r := httptest.NewRequest("POST", "/v1/actuator-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleActuatorModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup model POST: %d %s", w.Code, w.Body.String())
	}

	// 인스턴스 직접 store 로 삽입.
	ctx := r.Context()
	if err := s.store.UpsertActuator(ctx, &actuator.Actuator{
		DeviceID:   "pump_tank_01_01",
		DeviceType: "pump",
		TankID:     "tank_01",
		ModelID:    "m1",
	}); err != nil {
		t.Fatalf("seed actuator: %v", err)
	}

	r = httptest.NewRequest("DELETE", "/v1/actuator-models/m1", nil)
	w = httptest.NewRecorder()
	s.handleActuatorModelItem(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ACTUATOR_MODEL_IN_USE") {
		t.Fatalf("expected ACTUATOR_MODEL_IN_USE in body, got: %s", w.Body.String())
	}
}

// C-13b — POST /v1/actuators 가 새 필드 (model_id/mount_location/safety_roles/operating_mode)
// 정상 echo + invalid safety_role 거부.
func TestActuatorPostWithModelMetaAndSafetyEnum(t *testing.T) {
	s := newTestServer(t)
	s.app = runtime.NewApp(s.cfg, s.store)

	// 모델 먼저 등록.
	body := []byte(`{"model_id":"aerator_m1","vendor":"V","product_code":"P","display_name":"Aerator","device_category":"aerator"}`)
	r := httptest.NewRequest("POST", "/v1/actuator-models", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleActuatorModelsRoute(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup model POST: %d %s", w.Code, w.Body.String())
	}

	// 정상 POST — 새 필드 모두 포함.
	good := map[string]any{
		"device_id":      "aerator_tank_01_01",
		"device_type":    "aerator",
		"tank_id":        "tank_01",
		"model_id":       "aerator_m1",
		"mount_location": "tank_center",
		"safety_role":    []string{"oxygen_critical", "oxygen_backup"},
		"operating_mode": "auto",
	}
	body, _ = json.Marshal(good)
	r = httptest.NewRequest("POST", "/v1/actuators", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handlePostActuator(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("good actuator POST: got %d body=%s", w.Code, w.Body.String())
	}
	// echo 확인.
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	item, _ := resp["item"].(map[string]any)
	if item["model_id"] != "aerator_m1" {
		t.Fatalf("model_id echo: %v", item["model_id"])
	}
	if item["mount_location"] != "tank_center" {
		t.Fatalf("mount_location echo: %v", item["mount_location"])
	}

	// invalid safety_role 거부.
	bad := map[string]any{
		"device_id":   "bad_safety_01",
		"device_type": "pump",
		"safety_role": []string{"oxygen_critical", "telekinesis_drive"},
	}
	body, _ = json.Marshal(bad)
	r = httptest.NewRequest("POST", "/v1/actuators", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handlePostActuator(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid safety_role: got %d, want 422", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_SAFETY_ROLE") {
		t.Fatalf("expected INVALID_SAFETY_ROLE in body, got: %s", w.Body.String())
	}

	// invalid mount_location 거부.
	bad2 := map[string]any{
		"device_id":      "bad_mount_01",
		"device_type":    "pump",
		"mount_location": "moonbase",
	}
	body, _ = json.Marshal(bad2)
	r = httptest.NewRequest("POST", "/v1/actuators", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handlePostActuator(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid mount_location: got %d, want 422", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_MOUNT_LOCATION") {
		t.Fatalf("expected INVALID_MOUNT_LOCATION, got: %s", w.Body.String())
	}
}
