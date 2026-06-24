package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/controller"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// Service validates, records, and dispatches control commands.
type Service struct {
	app     *runtime.App
	cfg     *config.ControlConfig
	devices *config.DevicesConfig
	store   storage.Store
	mocks   map[string]*MockAdapter // adapter id -> mock
	cancel  context.CancelFunc
}

func NewService(app *runtime.App, cfg *config.ControlConfig, devices *config.DevicesConfig, store storage.Store) *Service {
	mocks := map[string]*MockAdapter{}
	for _, a := range cfg.Adapters {
		if a.Type == "mock_control" {
			mocks[a.ID] = NewMockAdapter(a)
		}
	}
	return &Service{app: app, cfg: cfg, devices: devices, store: store, mocks: mocks}
}

func (s *Service) Name() string { return "control" }

func (s *Service) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.app.Health.Set("control", "disabled", "control disabled by config")
		return nil
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.app.Health.Set("control", "ok", "")

	// Recover expired commands from previous runs
	go s.recoverExpiredCommands(ctx)
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

// Submit validates and records a command, then dispatches it to the mock adapter.
func (s *Service) Submit(ctx context.Context, req CommandRequest) (*CommandResult, error) {
	// 1. Idempotency check
	if s.cfg.RequireIdempotencyKey && req.IdempotencyKey == "" {
		return nil, &RejectionError{Code: "MISSING_IDEMPOTENCY_KEY", Message: "idempotency_key is required"}
	}
	if req.IdempotencyKey != "" {
		existing, err := s.store.GetCommandByIdempotencyKey(ctx, req.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return &CommandResult{CommandID: existing.CommandID, Status: existing.Status}, nil
		}
	}

	// 2. Validate target device
	deviceID, _ := req.Target["device_id"].(string)
	if deviceID == "" {
		return nil, &RejectionError{Code: "MISSING_DEVICE_ID", Message: "target.device_id is required"}
	}
	device := s.findDevice(deviceID)
	if device == nil {
		return nil, &RejectionError{Code: "UNKNOWN_DEVICE", Message: "device_id is not registered: " + deviceID}
	}

	// 3. Validate command type
	commandType, _ := req.Command["type"].(string)
	if commandType == "" {
		return nil, &RejectionError{Code: "MISSING_COMMAND_TYPE", Message: "command.type is required"}
	}
	if !hasCapability(device.Capabilities, commandType) {
		return nil, &RejectionError{Code: "UNSUPPORTED_COMMAND", Message: "device does not support command: " + commandType}
	}

	// 3b. Feed command–specific validation (Phase 3 new types)
	if params, _ := req.Command["params"].(map[string]any); params != nil {
		if err := ValidateFeedCommand(commandType, params); err != nil {
			return nil, &RejectionError{Code: "INVALID_FEED_PARAMS", Message: err.Error()}
		}
	}

	// 4. Automatic commands are rejected in Phase 1
	if s.cfg.AutomaticCommandsEnabled {
		return nil, &RejectionError{Code: "AUTOMATIC_CONTROL_DISABLED", Message: "automatic_commands_enabled=true is not allowed in Phase 1"}
	}

	// 5. Compute expiry
	ttl := s.cfg.DefaultCommandTTLSec
	if req.ExpiresInSec > 0 {
		ttl = req.ExpiresInSec
	}
	now := common.NowUTC()
	expiresAt := now.Add(time.Duration(ttl) * time.Second)

	commandID := common.NewCommandID()

	payloadJSON, _ := json.Marshal(map[string]any{
		"command_id":      commandID,
		"idempotency_key": req.IdempotencyKey,
		"requested_by":    req.RequestedBy,
		"target":          req.Target,
		"command":         req.Command,
		"safety":          map[string]any{"mode": "manual", "requires_approval": false},
		"requested_at":    common.FormatTime(now),
		"expires_at":      common.FormatTime(expiresAt),
	})

	// 6. Record requested event
	evtPayload := map[string]any{
		"command_id":      commandID,
		"idempotency_key": req.IdempotencyKey,
		"requested_by":    req.RequestedBy,
		"target":          req.Target,
		"command":         req.Command,
		"requested_at":    common.FormatTime(now),
		"expires_at":      common.FormatTime(expiresAt),
	}
	evtSeq, err := s.app.AppendEvent(ctx, "control", device.AdapterID, deviceID,
		"control.command.requested", commandID, evtPayload)
	if err != nil {
		return nil, err
	}
	eventID := s.seqToEventID(ctx, evtSeq)

	cmd := &storage.ControlCommand{
		CommandID:      commandID,
		IdempotencyKey: req.IdempotencyKey,
		TargetDeviceID: deviceID,
		CommandType:    commandType,
		Status:         "requested",
		RequestedAt:    now,
		ExpiresAt:      expiresAt,
		LastEventID:    eventID,
		PayloadJSON:    string(payloadJSON),
	}
	if err := s.store.InsertCommand(ctx, cmd); err != nil {
		return nil, err
	}

	// 7. Accept
	acceptEvtSeq, err := s.app.AppendEvent(ctx, "control", device.AdapterID, deviceID,
		"control.command.accepted", commandID, map[string]any{"command_id": commandID, "accepted_at": common.FormatTime(now)})
	if err != nil {
		slog.Warn("failed to record accept event", "command_id", commandID, "error", err)
	}
	acceptEventID := s.seqToEventID(ctx, acceptEvtSeq)
	s.store.UpdateCommandStatus(ctx, commandID, "accepted", acceptEventID)

	// 8. Dispatch in background
	go s.dispatch(commandID, deviceID, commandType, device.AdapterID, req.Command)

	return &CommandResult{CommandID: commandID, Status: "accepted"}, nil
}

// SubmitControllerTest enqueues a commissioning self-test command addressed to the
// controller itself (no devices.yaml entry needed — 동적 등록 컨트롤러 지원). ESP32 가
// 폴링해 DAC/STOP self-test 를 실행하고 결과를 POST 하면 RecordControllerResult 가
// controller.commissioning 을 갱신한다. 반환값은 command_id.
func (s *Service) SubmitControllerTest(ctx context.Context, controllerID string) (string, error) {
	now := common.NowUTC()
	ttl := s.cfg.DefaultCommandTTLSec
	if ttl <= 0 {
		ttl = 60
	}
	expiresAt := now.Add(time.Duration(ttl) * time.Second)
	commandID := common.NewCommandID()
	command := map[string]any{"type": "feeder.commissioning_test", "params": map[string]any{}}
	target := map[string]any{"device_id": controllerID}
	payloadJSON, _ := json.Marshal(map[string]any{
		"command_id":   commandID,
		"requested_by": "operator",
		"target":       target,
		"command":      command,
		"safety":       map[string]any{"mode": "manual", "requires_approval": false},
		"requested_at": common.FormatTime(now),
		"expires_at":   common.FormatTime(expiresAt),
	})
	evtSeq, err := s.app.AppendEvent(ctx, "control", "", controllerID,
		"control.command.requested", commandID, map[string]any{
			"command_id": commandID, "controller_id": controllerID, "command": command,
			"requested_at": common.FormatTime(now), "expires_at": common.FormatTime(expiresAt),
		})
	if err != nil {
		return "", err
	}
	cmd := &storage.ControlCommand{
		CommandID:      commandID,
		IdempotencyKey: commandID, // 매 테스트 고유 (idempotency_key UNIQUE 제약 충돌 방지)
		TargetDeviceID: controllerID,
		CommandType:    "feeder.commissioning_test",
		Status:         "queued",
		RequestedAt:    now,
		ExpiresAt:      expiresAt,
		LastEventID:    s.seqToEventID(ctx, evtSeq),
		PayloadJSON:    string(payloadJSON),
	}
	if err := s.store.InsertCommand(ctx, cmd); err != nil {
		return "", err
	}
	return commandID, nil
}

// NextForController returns the next queued command for a controller.
// A controller owns devices whose adapter_id equals controllerID. If the controller
// itself is registered as a device, it may also poll commands targeted at its own device_id.
func (s *Service) NextForController(ctx context.Context, controllerID string) (*storage.ControlCommand, error) {
	if controllerID == "" {
		return nil, &RejectionError{Code: "MISSING_CONTROLLER_ID", Message: "controller_id is required"}
	}
	deviceIDs := s.deviceIDsForController(controllerID)
	cmd, err := s.store.ListNextCommandForDevices(ctx, deviceIDs, common.NowUTC())
	if err != nil || cmd == nil {
		return cmd, err
	}
	s.recordCommandEvent(ctx, cmd.CommandID, cmd.TargetDeviceID, controllerID, "control.command.dispatched",
		map[string]any{"command_id": cmd.CommandID, "controller_id": controllerID, "dispatched_at": common.FormatTime(common.NowUTC())})
	s.store.UpdateCommandStatus(ctx, cmd.CommandID, "dispatched", "")
	return cmd, nil
}

func (s *Service) AckControllerCommand(ctx context.Context, controllerID, commandID string, ack map[string]any) error {
	cmd, err := s.store.GetCommand(ctx, commandID)
	if err != nil || cmd == nil {
		if err != nil {
			return err
		}
		return &RejectionError{Code: "UNKNOWN_COMMAND", Message: "command not found: " + commandID}
	}
	if !s.controllerOwnsDevice(controllerID, cmd.TargetDeviceID) {
		return &RejectionError{Code: "CONTROLLER_DEVICE_MISMATCH", Message: "controller does not own target device"}
	}
	now := common.NowUTC()
	s.recordCommandEvent(ctx, commandID, cmd.TargetDeviceID, controllerID, "control.command.acknowledged",
		map[string]any{"command_id": commandID, "controller_id": controllerID, "acknowledged_at": common.FormatTime(now), "ack": ack})
	return s.store.UpdateCommandStatus(ctx, commandID, "acknowledged", "")
}

func (s *Service) RecordControllerResult(ctx context.Context, controllerID, commandID, result string, details map[string]any) error {
	cmd, err := s.store.GetCommand(ctx, commandID)
	if err != nil || cmd == nil {
		if err != nil {
			return err
		}
		return &RejectionError{Code: "UNKNOWN_COMMAND", Message: "command not found: " + commandID}
	}
	if !s.controllerOwnsDevice(controllerID, cmd.TargetDeviceID) {
		return &RejectionError{Code: "CONTROLLER_DEVICE_MISMATCH", Message: "controller does not own target device"}
	}
	if result == "" {
		result = "succeeded"
	}
	status := result
	if result == "rejected" || result == "expired" {
		status = result
	}
	if result != "succeeded" && result != "failed" && result != "rejected" && result != "expired" {
		return &RejectionError{Code: "INVALID_RESULT", Message: "result must be succeeded, failed, rejected, or expired"}
	}
	now := common.NowUTC()
	s.recordCommandEvent(ctx, commandID, cmd.TargetDeviceID, controllerID, "control.command.result_recorded",
		map[string]any{"command_id": commandID, "controller_id": controllerID, "result": result, "completed_at": common.FormatTime(now), "details": details})

	// commissioning self-test 결과는 controller.commissioning 에 반영해 대시보드가 읽도록 한다.
	if cmd.CommandType == "feeder.commissioning_test" {
		if c, gerr := s.store.GetController(ctx, controllerID); gerr == nil && c != nil {
			comm := controller.Commissioning{}
			if v, ok := details["dac_ok"].(bool); ok {
				comm.DACOk = v
			}
			if v, ok := details["stop_ok"].(bool); ok {
				comm.StopOk = v
			}
			if v, ok := details["motor_ok"].(bool); ok {
				comm.MotorOk = v
			}
			if v, ok := details["latency_ms"].(float64); ok {
				lv := int(v)
				comm.LatencyMs = &lv
			}
			if v, ok := details["has_weight"].(bool); ok {
				comm.HasWeight = v
			}
			if v, ok := details["weight_g"].(float64); ok {
				wv := v
				comm.WeightG = &wv
			}
			if v, ok := details["tared"].(bool); ok {
				comm.Tared = v
			}
			ts := common.FormatTime(now)
			comm.TestedAt = &ts
			c.Commissioning = comm
			if uerr := s.store.UpsertController(ctx, c); uerr != nil {
				slog.Warn("commissioning update failed", "controller_id", controllerID, "error", uerr)
			}
		}
	}
	return s.store.UpdateCommandStatus(ctx, commandID, status, "")
}

func (s *Service) deviceIDsForController(controllerID string) []string {
	ids := []string{}
	if s.devices != nil {
		for _, d := range s.devices.Devices {
			if d.AdapterID == controllerID || d.DeviceID == controllerID {
				ids = append(ids, d.DeviceID)
			}
		}
	}
	// 동적 등록 컨트롤러(devices.yaml 에 없음)도 controller_id 로 주소 지정된 명령
	// (예: self-test)을 받도록 항상 controller_id 자체를 포함한다.
	ids = append(ids, controllerID)
	return ids
}

// DeviceIDsForController exposes the controller→device mapping for external callers
// (예: feed_cycle.Worker.dispatchPulse 가 controller_id 만 갖고 있을 때 device_id 로 변환).
func (s *Service) DeviceIDsForController(controllerID string) []string {
	return s.deviceIDsForController(controllerID)
}

func (s *Service) controllerOwnsDevice(controllerID, deviceID string) bool {
	for _, id := range s.deviceIDsForController(controllerID) {
		if id == deviceID {
			return true
		}
	}
	return false
}

func (s *Service) dispatch(commandID, deviceID, commandType, adapterID string, command map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adapter, ok := s.mocks[adapterID]
	if !ok {
		// Non-mock adapters are expected to be polled by ESP32/controller nodes.
		slog.Info("command queued for external controller", "adapter_id", adapterID, "command_id", commandID)
		s.recordCommandEvent(ctx, commandID, deviceID, adapterID, "control.command.queued",
			map[string]any{"command_id": commandID, "queued_at": common.FormatTime(common.NowUTC()), "dispatch_mode": "controller_polling"})
		s.store.UpdateCommandStatus(ctx, commandID, "queued", "")
		return
	}

	// Sent
	sentAt := common.NowUTC()
	s.recordCommandEvent(ctx, commandID, deviceID, adapterID, "control.command.sent",
		map[string]any{"command_id": commandID, "sent_at": common.FormatTime(sentAt)})
	s.store.UpdateCommandStatus(ctx, commandID, "sent", "")

	params, _ := command["params"].(map[string]any)
	ack, err := adapter.Execute(ctx, commandID, commandType, params)
	completedAt := common.NowUTC()

	if err != nil {
		s.recordCommandEvent(ctx, commandID, deviceID, adapterID, "control.command.failed",
			map[string]any{
				"command_id": commandID,
				"result":     "failed",
				"sent_at":    common.FormatTime(sentAt),
				"failed_at":  common.FormatTime(completedAt),
				"error":      map[string]any{"code": "ADAPTER_ERROR", "message": err.Error()},
				"retryable":  false,
			})
		s.store.UpdateCommandStatus(ctx, commandID, "failed", "")
		return
	}

	s.recordCommandEvent(ctx, commandID, deviceID, adapterID, "control.command.succeeded",
		map[string]any{
			"command_id":   commandID,
			"result":       "succeeded",
			"sent_at":      common.FormatTime(sentAt),
			"completed_at": common.FormatTime(completedAt),
			"device_ack":   ack,
		})
	s.store.UpdateCommandStatus(ctx, commandID, "succeeded", "")
}

func (s *Service) recordCommandEvent(ctx context.Context, commandID, deviceID, adapterID, eventType string, payload map[string]any) {
	if _, err := s.app.AppendEvent(ctx, "control", adapterID, deviceID, eventType, commandID, payload); err != nil {
		slog.Warn("failed to record command event", "event_type", eventType, "command_id", commandID, "error", err)
	}
}

func (s *Service) recoverExpiredCommands(ctx context.Context) {
	expired, err := s.store.ListExpiredCommands(ctx, common.NowUTC())
	if err != nil {
		slog.Warn("failed to list expired commands", "error", err)
		return
	}
	for _, cmd := range expired {
		slog.Info("expiring stale command", "command_id", cmd.CommandID)
		payload := map[string]any{
			"command_id": cmd.CommandID,
			"reason":     "expired at startup",
			"expired_at": common.FormatTime(common.NowUTC()),
		}
		if _, err := s.app.AppendEvent(ctx, "control", "", cmd.TargetDeviceID,
			"control.command.expired", cmd.CommandID, payload); err != nil {
			slog.Warn("failed to record expiry event", "error", err)
		}
		s.store.UpdateCommandStatus(ctx, cmd.CommandID, "expired", "")
	}
}

func (s *Service) findDevice(deviceID string) *config.DeviceEntry {
	if s.devices == nil {
		return nil
	}
	for i := range s.devices.Devices {
		if s.devices.Devices[i].DeviceID == deviceID {
			return &s.devices.Devices[i]
		}
	}
	return nil
}

func hasCapability(caps []string, cmd string) bool {
	for _, c := range caps {
		if c == cmd {
			return true
		}
	}
	return false
}

func (s *Service) seqToEventID(ctx context.Context, seq int64) string {
	events, err := s.store.QueryEvents(ctx, storage.EventFilter{AfterSeq: seq - 1, Limit: 1})
	if err != nil || len(events) == 0 {
		return ""
	}
	return events[0].EventID
}
