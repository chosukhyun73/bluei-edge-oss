package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bluei.kr/edge/internal/controller"
)

// 자원 매핑(POST /v1/actuators)이 controller.tank_id 를 동기화하는지 검증.
// 등록 후 controller.tank_id 를 갱신할 다른 경로가 없어 매핑이 유일한 연결 수단이다.
func TestActuatorPostSyncsControllerTank(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	// 펌웨어 등록 시점의 (이제는 stale 한) tank_id 로 controller 선등록.
	if err := s.store.UpsertController(ctx, &controller.Controller{
		ControllerID:    "feeder_x",
		TankID:          "stale_tank",
		MACAddress:      "AA:BB:CC:DD:EE:FF",
		FirmwareVersion: "v0.4.0",
		Status:          controller.StatusPending,
		RegisteredAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed controller: %v", err)
	}

	// 운영자가 탱크 상세에서 이 controller 를 tank_real 에 매핑(장비 추가).
	body, _ := json.Marshal(map[string]any{
		"device_id":     "feeder_x_01",
		"device_type":   "feeder",
		"tank_id":       "tank_real",
		"controller_id": "feeder_x",
	})
	r := httptest.NewRequest("POST", "/v1/actuators", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePostActuator(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("actuator POST: got %d body=%s", w.Code, w.Body.String())
	}

	// controller.tank_id 가 매핑된 tank 로 동기화되어야 한다.
	c, err := s.store.GetController(ctx, "feeder_x")
	if err != nil {
		t.Fatalf("get controller: %v", err)
	}
	if c.TankID != "tank_real" {
		t.Fatalf("controller tank_id not synced: got %q want tank_real", c.TankID)
	}
}

// controller_id 없는 actuator 매핑은 controller 를 건드리지 않고 정상 처리.
func TestActuatorPostWithoutControllerNoSync(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(map[string]any{
		"device_id":   "pump_01",
		"device_type": "pump",
		"tank_id":     "tank_real",
	})
	r := httptest.NewRequest("POST", "/v1/actuators", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePostActuator(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("actuator POST: got %d body=%s", w.Code, w.Body.String())
	}
}
