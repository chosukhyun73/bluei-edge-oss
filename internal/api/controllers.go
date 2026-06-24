package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/arbiter"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/controller"
)

type controllerCommandAckRequest struct {
	Ack map[string]any `json:"ack"`
}

type controllerCommandResultRequest struct {
	Result  string         `json:"result"`
	Details map[string]any `json:"details"`
}

type controllerRegisterRequest struct {
	MACAddress      string `json:"mac_address"`
	ControllerID    string `json:"controller_id"`
	FirmwareVersion string `json:"firmware_version"`
	TankID          string `json:"tank_id,omitempty"`
	SiteID          string `json:"site_id,omitempty"`
}

func (s *Server) handleControllerRoute(w http.ResponseWriter, r *http.Request) {
	// POST /v1/controllers/register (no controller_id segment)
	if r.URL.Path == "/v1/controllers/register" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePostControllerRegister(w, r)
		return
	}

	// GET /v1/controllers (list) — path is exactly "/v1/controllers" but mux routes "/v1/controllers/" here too
	if r.URL.Path == "/v1/controllers" || r.URL.Path == "/v1/controllers/" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleGetControllers(w, r)
		return
	}

	controllerID, commandID, action, ok := parseControllerPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "controller route not found", "")
		return
	}

	// POST /v1/controllers/{id}/activate
	// DELETE /v1/controllers/{id}
	if commandID == "" && action == "" && r.Method == http.MethodDelete {
		s.handleDeleteController(w, r, controllerID)
		return
	}
	if commandID == "" && action == "activate" && r.Method == http.MethodPost {
		s.handlePostControllerActivate(w, r, controllerID)
		return
	}
	// POST /v1/controllers/{id}/test
	if commandID == "" && action == "test" && r.Method == http.MethodPost {
		s.handlePostControllerTest(w, r, controllerID)
		return
	}
	// POST /v1/controllers/{id}/weight — Phase 5 HX711 reading ingest
	if commandID == "" && action == "weight" && r.Method == http.MethodPost {
		s.handlePostControllerWeight(w, r, controllerID)
		return
	}
	// POST /v1/controllers/{id}/manual-trigger — r7: 운영자 수동 trigger 버튼 (momentary)
	if commandID == "" && action == "manual-trigger" && r.Method == http.MethodPost {
		s.handlePostControllerManualTrigger(w, r, controllerID)
		return
	}
	// GET /v1/controllers/{id}/live-weight — B-7: UDP weight cache (1Hz fresh)
	if commandID == "" && action == "live-weight" && r.Method == http.MethodGet {
		s.handleGetControllerLiveWeight(w, r, controllerID)
		return
	}

	if commandID == "" && action == "commands/next" && r.Method == http.MethodGet {
		s.handleGetControllerNextCommand(w, r, controllerID)
		return
	}
	if commandID != "" && action == "ack" && r.Method == http.MethodPost {
		s.handlePostControllerCommandAck(w, r, controllerID, commandID)
		return
	}
	if commandID != "" && action == "result" && r.Method == http.MethodPost {
		s.handlePostControllerCommandResult(w, r, controllerID, commandID)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported controller route", "")
}

func (s *Server) handlePostControllerRegister(w http.ResponseWriter, r *http.Request) {
	var req controllerRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.MACAddress == "" || req.ControllerID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "mac_address and controller_id are required", "")
		return
	}

	ctx := r.Context()
	existing, err := s.store.GetControllerByMAC(ctx, req.MACAddress)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}

	if existing != nil {
		if existing.ControllerID != req.ControllerID {
			writeError(w, http.StatusConflict, "MAC_CONFLICT", "MAC address already registered with different controller_id", "")
			return
		}
		// Same MAC + same controller_id: update last_seen_at + firmware_version (펌웨어 업그레이드 인지).
		existing.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
		if req.FirmwareVersion != "" {
			existing.FirmwareVersion = req.FirmwareVersion
		}
		if ip := extractRemoteIP(r.RemoteAddr); ip != "" {
			existing.IPAddress = ip
		}
		if err := s.store.UpsertController(ctx, existing); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"controller_id": existing.ControllerID,
			"status":        string(existing.Status),
			"registered_at": existing.RegisteredAt,
		})
		return
	}

	// New registration.
	now := time.Now().UTC().Format(time.RFC3339)
	c := &controller.Controller{
		ControllerID:    req.ControllerID,
		MACAddress:      req.MACAddress,
		IPAddress:       extractRemoteIP(r.RemoteAddr),
		FirmwareVersion: req.FirmwareVersion,
		TankID:          req.TankID,
		SiteID:          req.SiteID,
		Status:          controller.StatusPending,
		RegisteredAt:    now,
		LastSeenAt:      now,
	}
	if err := s.store.UpsertController(ctx, c); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"controller_id": c.ControllerID,
		"status":        string(c.Status),
		"registered_at": c.RegisteredAt,
	})
}

func (s *Server) handlePostControllerActivate(w http.ResponseWriter, r *http.Request, controllerID string) {
	ctx := r.Context()
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "controller not found", "")
		return
	}
	if c.Status != controller.StatusPending {
		writeError(w, http.StatusConflict, "INVALID_TRANSITION",
			"only pending controllers can be activated; current status: "+string(c.Status), "")
		return
	}
	c.Status = controller.StatusActive
	if err := s.store.UpsertController(ctx, c); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"controller_id": c.ControllerID,
		"status":        string(c.Status),
	})
}

// handleDeleteController removes a controller registration. 운영자가 잘못 등록되거나
// 폐기된 controller 를 제거하는 용도. ESP32 가 다시 register 하면 재등록되는 것은 정상.
func (s *Server) handleDeleteController(w http.ResponseWriter, r *http.Request, controllerID string) {
	ctx := r.Context()
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "controller not found", "")
		return
	}
	if err := s.store.DeleteController(ctx, controllerID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": controllerID})
}

func (s *Server) handlePostControllerTest(w http.ResponseWriter, r *http.Request, controllerID string) {
	ctx := r.Context()
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "controller not found", "")
		return
	}
	// 실제 self-test: feeder.commissioning_test 명령을 enqueue → ESP32 가 폴링해 실행 후
	// 결과를 POST 하면 RecordControllerResult 가 commissioning 을 갱신한다 (비동기).
	// 프론트는 controller.commissioning.tested_at 갱신을 폴링해 결과를 표시한다.
	cmdID, err := s.ctrl.SubmitControllerTest(ctx, controllerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"controller_id": c.ControllerID,
		"command_id":    cmdID,
		"status":        "pending",
		"message":       "테스트 명령을 전송했습니다 — 컨트롤러가 실행 후 결과를 보고합니다.",
	})
}

func (s *Server) handleGetControllers(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	items, err := s.store.ListControllers(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*controller.Controller{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetControllerNextCommand(w http.ResponseWriter, r *http.Request, controllerID string) {
	s.touchControllerLastSeen(r.Context(), controllerID, r.RemoteAddr)
	cmd, err := s.ctrl.NextForController(r.Context(), controllerID)
	if err != nil {
		writeControllerError(w, err)
		return
	}
	if cmd == nil {
		writeJSON(w, http.StatusOK, map[string]any{"command": nil})
		return
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(cmd.PayloadJSON), &payload)
	writeJSON(w, http.StatusOK, map[string]any{
		"command": map[string]any{
			"command_id":       cmd.CommandID,
			"target_device_id": cmd.TargetDeviceID,
			"command_type":     cmd.CommandType,
			"expires_at":       cmd.ExpiresAt,
			"payload":          payload,
		},
	})
}

func (s *Server) handlePostControllerCommandAck(w http.ResponseWriter, r *http.Request, controllerID, commandID string) {
	var req controllerCommandAckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.Ack == nil {
		req.Ack = map[string]any{}
	}
	if err := s.ctrl.AckControllerCommand(r.Context(), controllerID, commandID, req.Ack); err != nil {
		writeControllerError(w, err)
		return
	}
	s.touchControllerLastSeen(r.Context(), controllerID, r.RemoteAddr)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "command_id": commandID, "status": "acknowledged"})
}

func (s *Server) handlePostControllerCommandResult(w http.ResponseWriter, r *http.Request, controllerID, commandID string) {
	var req controllerCommandResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.Details == nil {
		req.Details = map[string]any{}
	}
	if err := s.ctrl.RecordControllerResult(r.Context(), controllerID, commandID, req.Result, req.Details); err != nil {
		writeControllerError(w, err)
		return
	}
	s.touchControllerLastSeen(r.Context(), controllerID, r.RemoteAddr)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "command_id": commandID, "result": req.Result})
}

// touchControllerLastSeen — polling/ack/result 시 last_seen_at 갱신 + IP 자동 추적.
// remoteAddr 형식: "192.168.0.124:54321" → IP 만 추출. 실패 시 IP 변경 안 함.
// 부담: SQLite write 한 번. polling 3초 주기 × controllers N개 = N/3 writes/sec — 무시할 수준.
func (s *Server) touchControllerLastSeen(ctx context.Context, controllerID, remoteAddr string) {
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil || c == nil {
		return
	}
	c.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	if ip := extractRemoteIP(remoteAddr); ip != "" && c.IPAddress != ip {
		c.IPAddress = ip
	}
	_ = s.store.UpsertController(ctx, c)
}

// B-7: GET /v1/controllers/{id}/live-weight — UDP weight stream cache (1Hz).
// 응답: { grams, raw, mode, rssi, age_ms } 또는 503 (provider 미설정/패킷 없음).
// 폴링 1Hz 권장 — ESP32 push 주기와 일치.
func (s *Server) handleGetControllerLiveWeight(w http.ResponseWriter, r *http.Request, controllerID string) {
	if s.liveWeight == nil {
		writeError(w, http.StatusServiceUnavailable, "LIVE_WEIGHT_UNAVAILABLE", "udp listener not wired", "")
		return
	}
	grams, rawGrams, raw, mode, rssi, ageMs, ok := s.liveWeight.GetLiveWeight(controllerID)
	if !ok {
		writeError(w, http.StatusNotFound, "NO_WEIGHT", "no UDP weight packet received for "+controllerID, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"controller_id": controllerID,
		"grams":         grams,
		"raw_grams":     rawGrams,
		"raw":           raw,
		"mode":          mode,
		"rssi":          rssi,
		"age_ms":        ageMs,
	})
}

// r7: 운영자 수동 trigger 버튼 — momentary push button (GPIO 19) 누름 시 ESP32 가 호출.
// backend 가 controller → tank_id resolve → arbiter 경유 standard adaptive cycle 발행.
// 안전 게이트 (temp/DO/sensor stale) + 일일 사료량 limit + 기존 cycle 충돌 검사 모두 적용.
func (s *Server) handlePostControllerManualTrigger(w http.ResponseWriter, r *http.Request, controllerID string) {
	ctx := r.Context()
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "controller not found", "")
		return
	}
	if c.TankID == "" {
		writeError(w, http.StatusConflict, "NO_TANK", "controller not assigned to tank", "")
		return
	}
	s.touchControllerLastSeen(ctx, controllerID, r.RemoteAddr)

	// 테스트용 cycle 파라미터 (운영자 수동 trigger).
	// 운전 10s + 갭 5s × 4회 = 약 55초. max_pulses 도달 시 자동 종료.
	// 강릉 운영 시 species_profile 또는 환경별 default 로 교체 가능.
	// Phase 5 — stop_at_remaining_g 추가. 통 잔량 ≤ 1000g 면 즉시 종료.
	params := map[string]any{
		"target_amount_g":     1000.0, // 큰 값 = max_pulses 우선 종료
		"max_pulses":          4,
		"max_duration_min":    1,
		"pulse_duration_ms":   10000, // 10초 운전
		"gap_ms":              5000,  // 5초 갭
		"stop_at_remaining_g": 1000.0,
	}

	if s.arbiter == nil {
		writeError(w, http.StatusServiceUnavailable, "ARBITER_UNAVAILABLE", "arbiter not configured", "")
		return
	}
	dec, err := s.arbiter.Submit(ctx, arbiter.CycleRequest{
		TankID:       c.TankID,
		ControllerID: controllerID,
		Source:       arbiter.SourceOperatorManual,
		Mode:         "adaptive",
		Params:       params,
		SubmittedAt:  time.Now().UTC(),
		IntentID:     "",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CYCLE_START_FAILED", err.Error(), "")
		return
	}
	if !dec.Accepted {
		code := "CYCLE_CONFLICT"
		if strings.HasPrefix(dec.RejectionReason, "safety_gate:") {
			code = "SAFETY_GATE_BLOCKED"
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok":               false,
			"controller_id":    controllerID,
			"source":           "manual_trigger_button",
			"rejection_reason": dec.RejectionReason,
			"decision_id":      dec.DecisionID,
			"error":            map[string]any{"code": code, "message": dec.RejectionReason},
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":            true,
		"controller_id": controllerID,
		"tank_id":       c.TankID,
		"cycle_id":      dec.ResultingCycleID,
		"source":        "manual_trigger_button",
		"decision_id":   dec.DecisionID,
	})
}

// extractRemoteIP — "host:port" → "host". IPv6 형식 "[::1]:port" 도 처리.
func extractRemoteIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	// IPv6 "[::1]:port"
	if strings.HasPrefix(remoteAddr, "[") {
		if end := strings.Index(remoteAddr, "]"); end > 0 {
			return remoteAddr[1:end]
		}
	}
	if idx := strings.LastIndex(remoteAddr, ":"); idx >= 0 {
		return remoteAddr[:idx]
	}
	return remoteAddr
}

// Phase 5 (load cell) — ESP32 가 보내는 HX711 weight reading payload.
type controllerWeightRequest struct {
	PulseID       string  `json:"pulse_id"`
	WeightBeforeG float64 `json:"weight_before_g"`
	WeightAfterG  float64 `json:"weight_after_g"`
	MeasuredAt    string  `json:"measured_at"` // RFC3339; 미지정 시 server now
}

func (s *Server) handlePostControllerWeight(w http.ResponseWriter, r *http.Request, controllerID string) {
	var req controllerWeightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.PulseID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "pulse_id required", "")
		return
	}
	if req.WeightBeforeG < 0 || req.WeightAfterG < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_RANGE", "weight_before_g/weight_after_g must be >= 0", "")
		return
	}
	if s.feedCycle == nil {
		writeError(w, http.StatusServiceUnavailable, "FEED_CYCLE_UNAVAILABLE", "feed_cycle worker not initialized", "")
		return
	}
	measuredAt, _ := time.Parse(time.RFC3339Nano, req.MeasuredAt)
	if measuredAt.IsZero() {
		measuredAt = time.Now().UTC()
	}
	res, cycle := s.feedCycle.RecordWeight(r.Context(), controllerID, req.PulseID, req.WeightBeforeG, req.WeightAfterG, measuredAt)
	if res == nil {
		// active cycle 없음 — pulse 도착이 finalize 보다 늦었거나, 사이클 외 측정. 200 accept + no-op.
		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":            true,
			"controller_id": controllerID,
			"active_cycle":  false,
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":                      true,
		"controller_id":           controllerID,
		"cycle_id":                cycle.CycleID,
		"delta_g":                 res.DeltaG,
		"actual_total_amount_g":   res.ActualTotalAmountG,
		"expected_g":              res.ExpectedG,
		"silo_depletion_detected": res.SiloDepletionDetect,
		"overflow_detected":       res.OverflowDetect,
	})
}

func parseControllerPath(path string) (controllerID, commandID, action string, ok bool) {
	prefix := "/v1/controllers/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", "", false
	}
	// /v1/controllers/{id} (no action) — DELETE
	if len(parts) == 1 {
		return parts[0], "", "", true
	}
	// /v1/controllers/{id}/activate | /test | /weight | /manual-trigger
	if len(parts) == 2 && (parts[1] == "activate" || parts[1] == "test" || parts[1] == "weight" || parts[1] == "manual-trigger" || parts[1] == "live-weight") {
		return parts[0], "", parts[1], true
	}
	// /v1/controllers/{id}/commands/next
	if len(parts) == 3 && parts[1] == "commands" && parts[2] == "next" {
		return parts[0], "", "commands/next", true
	}
	// /v1/controllers/{id}/commands/{cmd_id}/ack|result
	if len(parts) == 4 && parts[1] == "commands" && (parts[3] == "ack" || parts[3] == "result") {
		return parts[0], parts[2], parts[3], true
	}
	return "", "", "", false
}

func writeControllerError(w http.ResponseWriter, err error) {
	if rej, ok := err.(*control.RejectionError); ok {
		writeError(w, http.StatusUnprocessableEntity, rej.Code, rej.Message, "")
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), "")
}
