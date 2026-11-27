package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/controller"
)

// onoff 급이기 등록/제어 — ESP32(MAC 자기등록 + 명령 폴링)와 별개 경로.
// 경연대회는 외부 장비 API 로 제어한다: 급이기는 setpoint control(사료급이 setpoint,
// {ext_id, value}). chiller_control.go(switch control) 와 같은 인라인 onoff_feeder 호출 패턴.
// ext_id 는 컨트롤러 Metadata["feeder_ext_id"] 에 저장(스키마 변경 불필요).

type feederRegisterRequest struct {
	ControllerID string `json:"controller_id"`
	TankID       string `json:"tank_id"`
	FeederExtID  int    `json:"feeder_ext_id"`
}

// handleRegisterOnoffFeeder — POST /v1/controllers/feeder-register
// onoff 급이기 등록(MAC 불필요). tank + 사료급이 setpoint ext_id 매핑.
func (s *Server) handleRegisterOnoffFeeder(w http.ResponseWriter, r *http.Request) {
	var req feederRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.ControllerID == "" || req.TankID == "" || req.FeederExtID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_FEEDER",
			"controller_id, tank_id, feeder_ext_id(>0) required", "")
		return
	}
	ctx := r.Context()
	now := common.FormatTime(common.NowUTC())
	c, _ := s.store.GetController(ctx, req.ControllerID)
	if c == nil {
		c = &controller.Controller{ControllerID: req.ControllerID, RegisteredAt: now}
	}
	c.TankID = req.TankID
	c.Status = controller.StatusActive
	if c.FirmwareVersion == "" {
		c.FirmwareVersion = "onoff_feeder-api"
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	c.Metadata["control_backend"] = "onoff_feeder"
	c.Metadata["feeder_ext_id"] = req.FeederExtID
	c.LastSeenAt = now
	if err := s.store.UpsertController(ctx, c); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	slog.Info("onoff feeder registered", "controller_id", req.ControllerID, "tank_id", req.TankID, "ext_id", req.FeederExtID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "controller_id": req.ControllerID, "tank_id": req.TankID,
		"feeder_ext_id": req.FeederExtID, "control_backend": "onoff_feeder",
	})
}

// handleFeederControl — POST /v1/controllers/{id}/feed  body {action:"run"|"stop", confirm}
// onoff feeder control (사료급이기 운전/정지) 로 제어. 급이기는 setpoint 쓰기(setpoint control)가
// 아니라 run/stop 액션이다 (외부 장비 API feeder control, 관리 화면 소스 setpoint 직접쓰기는 금지).
// ⚠ confirm=true 일 때만 실작동. 기본은 dry-run(매핑만 반환).
func (s *Server) handleFeederControl(w http.ResponseWriter, r *http.Request, controllerID string) {
	var body struct {
		Action  string `json:"action"` // "run" | "stop"
		Confirm bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if body.Action != "run" && body.Action != "stop" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_ACTION", "action must be 'run' or 'stop'", "")
		return
	}
	ctx := r.Context()
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "controller not found", "")
		return
	}
	extID := metaInt(c.Metadata, "feeder_ext_id")
	if extID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "NOT_ONOFF_FEEDER",
			"controller has no feeder_ext_id (onoff 급이기로 등록되지 않음)", "")
		return
	}

	if !body.Confirm {
		// dry-run — 실작동 없이 매핑만 반환(안전 검증용).
		writeJSON(w, http.StatusOK, map[string]any{
			"dry_run": true, "controller_id": controllerID, "tank_id": c.TankID,
			"ext_id": extID, "endpoint": "feeder control", "action": body.Action,
			"note": "실작동하려면 confirm:true. onoff feeder control 로 급이기 운전/정지.",
		})
		return
	}

	adapter := control.NewOnoffFeederControlAdapter(config.ControlAdapter{ID: "feeder-manual"})
	cmdID := common.NewID("cmd")
	ack, err := adapter.Execute(ctx, cmdID, "feeder."+body.Action, map[string]any{
		"ext_id": float64(extID),
	})
	if err != nil {
		slog.Error("onoff feeder control failed", "controller_id", controllerID, "ext_id", extID, "action", body.Action, "error", err)
		writeError(w, http.StatusBadGateway, "FEEDER_CONTROL_FAILED", err.Error(), "")
		return
	}
	slog.Info("onoff feeder control sent", "controller_id", controllerID, "ext_id", extID, "action", body.Action, "command_id", cmdID)

	// 2026-07-04: 운영자의 원시 "정지" = 전부 멈춰. 급이기만 끄면 진행 중 사이클이
	// 다음 펄스에서 재가동한다(실증: 12:03 만복 세션 — 정지 19초 후 재점화).
	// 해당 수조의 활성 사이클도 함께 정지시킨다.
	var stoppedCycles []string
	if body.Action == "stop" && s.feedCycle != nil {
		if actives, aerr := s.store.ListActiveFeedCycles(ctx); aerr == nil {
			for _, ac := range actives {
				if ac.TankID != c.TankID {
					continue
				}
				if s.feedCycle.StopCycle(ac.CycleID) {
					stoppedCycles = append(stoppedCycles, ac.CycleID)
					slog.Info("active cycle stopped by raw feeder stop", "cycle_id", ac.CycleID, "tank_id", c.TankID)
				}
			}
		}
	}

	resp := map[string]any{
		"ok": true, "controller_id": controllerID, "ext_id": extID, "action": body.Action,
		"command_id": cmdID, "ack": ack,
	}
	if len(stoppedCycles) > 0 {
		resp["stopped_cycles"] = stoppedCycles
	}
	writeJSON(w, http.StatusOK, resp)
}

// metaInt — Metadata(map, JSON 언마샬 시 숫자는 float64)에서 int 추출.
func metaInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
