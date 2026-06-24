package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/arbiter"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
)

// POST /v1/feed-cycles
// GET  /v1/feed-cycles?tank_id=...&status=active|completed
func (s *Server) handleFeedCyclesRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleStartFeedCycle(w, r)
	case http.MethodGet:
		s.handleListFeedCycles(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET  /v1/feed-cycles/{cycle_id}
// POST /v1/feed-cycles/{cycle_id}/stop
func (s *Server) handleFeedCycleRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/feed-cycles/")
	if strings.HasSuffix(path, "/stop") {
		cycleID := strings.TrimSuffix(path, "/stop")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleStopFeedCycle(w, r, cycleID)
		return
	}
	// GET /v1/feed-cycles/{cycle_id}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handleGetFeedCycle(w, r, path)
}

// startFeedCycleRequest — POST /v1/feed-cycles body.
type startFeedCycleRequest struct {
	TankID       string         `json:"tank_id"`
	ControllerID string         `json:"controller_id"`
	Mode         string         `json:"mode"` // "adaptive" | "fixed"
	Params       map[string]any `json:"params"`
	IntentID     string         `json:"intent_id"` // 운영자 의도 메모 연결 (선택)
}

func (s *Server) handleStartFeedCycle(w http.ResponseWriter, r *http.Request) {
	if s.feedCycle == nil {
		writeError(w, http.StatusServiceUnavailable, "FEED_CYCLE_DISABLED", "feed cycle worker is not enabled", "")
		return
	}

	var req startFeedCycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.TankID == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_TANK_ID", "tank_id is required", "")
		return
	}
	if req.Mode != "adaptive" && req.Mode != "fixed" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_MODE", "mode must be 'adaptive' or 'fixed'", "")
		return
	}

	// Arbiter 경유: arbiter가 설정된 경우 우선순위 중재 후 사이클 시작
	if s.arbiter != nil {
		dec, err := s.arbiter.Submit(r.Context(), arbiter.CycleRequest{
			TankID:       req.TankID,
			ControllerID: req.ControllerID,
			Source:       arbiter.SourceOperatorManual,
			Mode:         req.Mode,
			Params:       req.Params,
			SubmittedAt:  time.Now().UTC(),
			IntentID:     req.IntentID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CYCLE_START_FAILED", err.Error(), "")
			return
		}
		if !dec.Accepted {
			// safety_gate:* prefix 면 환경 safety gate 위반 — 운영자 안내 명확화 필요.
			// 그 외 (active_cycle_exists / preempt_failed) 는 사이클 충돌 의미.
			if strings.HasPrefix(dec.RejectionReason, "safety_gate:") {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error": map[string]any{
						"code":             "SAFETY_GATE_BLOCKED",
						"message":          "blocked by environmental safety gate",
						"rejection_reason": dec.RejectionReason,
						"decision_id":      dec.DecisionID,
					},
				})
				return
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": map[string]any{
					"code":              "CYCLE_CONFLICT",
					"message":           "a cycle is already active for this tank",
					"existing_cycle_id": dec.ExistingCycleID,
					"rejection_reason":  dec.RejectionReason,
					"decision_id":       dec.DecisionID,
				},
			})
			return
		}
		// 수락됨
		resp := map[string]any{
			"cycle_id":    dec.ResultingCycleID,
			"status":      "active",
			"tank_id":     req.TankID,
			"mode":        req.Mode,
			"decision_id": dec.DecisionID,
		}
		if dec.PreemptedCycleID != "" {
			resp["preempted_cycle_id"] = dec.PreemptedCycleID
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Arbiter 미설정 시 기존 동작 유지 (하위 호환)
	var c *feed_cycle.Cycle
	var err error

	switch req.Mode {
	case "adaptive":
		p, vErr := parseAdaptiveParams(req.Params)
		if vErr != nil {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_PARAMS", vErr.Error(), "")
			return
		}
		c, err = s.feedCycle.StartAdaptiveCycle(r.Context(), req.TankID, req.ControllerID, p, "manual_override", req.IntentID)
	case "fixed":
		p, vErr := parseFixedParams(req.Params)
		if vErr != nil {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_PARAMS", vErr.Error(), "")
			return
		}
		c, err = s.feedCycle.StartFixedCycle(r.Context(), req.TankID, req.ControllerID, p, "manual_override", req.IntentID)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "CYCLE_START_FAILED", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"cycle_id": c.CycleID,
		"status":   string(c.State()),
		"tank_id":  c.TankID,
		"mode":     string(c.Mode),
	})
}

func (s *Server) handleListFeedCycles(w http.ResponseWriter, r *http.Request) {
	tankID := r.URL.Query().Get("tank_id")
	status := r.URL.Query().Get("status") // "active" | "completed" | ""

	var cycles []*storage.FeedCycle
	var err error

	switch status {
	case "active":
		cycles, err = s.store.ListActiveFeedCycles(r.Context())
		if err == nil && tankID != "" {
			// 클라이언트 필터 — active 목록은 소량
			filtered := cycles[:0]
			for _, c := range cycles {
				if c.TankID == tankID {
					filtered = append(filtered, c)
				}
			}
			cycles = filtered
		}
	default:
		if tankID == "" {
			writeError(w, http.StatusBadRequest, "MISSING_TANK_ID", "tank_id is required when status is not 'active'", "")
			return
		}
		limit := intParam(r, "limit", 20, 100)
		cycles, err = s.store.ListRecentFeedCycles(r.Context(), tankID, limit)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if cycles == nil {
		cycles = []*storage.FeedCycle{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.feedCycleViews(r, cycles)})
}

func (s *Server) handleGetFeedCycle(w http.ResponseWriter, r *http.Request, cycleID string) {
	cycle, err := s.store.GetFeedCycle(r.Context(), cycleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if cycle == nil {
		writeError(w, http.StatusNotFound, "CYCLE_NOT_FOUND", "cycle not found: "+cycleID, "")
		return
	}
	writeJSON(w, http.StatusOK, s.feedCycleView(r, cycle))
}

func (s *Server) handleStopFeedCycle(w http.ResponseWriter, r *http.Request, cycleID string) {
	if s.feedCycle == nil {
		writeError(w, http.StatusServiceUnavailable, "FEED_CYCLE_DISABLED", "feed cycle worker is not enabled", "")
		return
	}

	ok := s.feedCycle.StopCycle(cycleID)
	if !ok {
		// cycle이 in-memory에 없으면 DB에서 확인
		cycle, err := s.store.GetFeedCycle(r.Context(), cycleID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if cycle == nil {
			writeError(w, http.StatusNotFound, "CYCLE_NOT_FOUND", "cycle not found: "+cycleID, "")
			return
		}
		// Worker 메모리에 없는데 DB 에 active (completed_at IS NULL) 인 경우 — orphan cycle.
		// backend 재가동 등으로 worker 메모리 잃었을 때 발생. DB 강제 마킹으로 정리.
		if cycle.CompletedAt == nil {
			now := time.Now().UTC()
			if err := s.store.CompleteFeedCycle(r.Context(), cycleID,
				cycle.PulsesExecuted, cycle.TotalAmountG,
				"operator_stop_orphan", now); err != nil {
				writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"cycle_id":           cycleID,
				"status":             "completed",
				"termination_reason": "operator_stop_orphan",
				"note":               "orphan cycle (worker memory lost) — DB 강제 마킹",
			})
			return
		}
		// 이미 정상 완료된 사이클
		writeJSON(w, http.StatusOK, map[string]any{
			"cycle_id":           cycleID,
			"status":             "completed",
			"termination_reason": cycle.TerminationReason,
			"note":               "cycle was already completed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cycle_id":           cycleID,
		"status":             "completing",
		"termination_reason": "operator_stop",
	})
}

// --- view helpers -----------------------------------------------------------

// feedCycleView serialize 한 cycle 을 JSON 으로 변환한다.
// intent_id 가 있으면 operator_intents 에서 reason 을 inline 조회한다.
func (s *Server) feedCycleView(r *http.Request, c *storage.FeedCycle) map[string]any {
	v := map[string]any{
		"cycle_id":           c.CycleID,
		"tank_id":            c.TankID,
		"controller_id":      c.ControllerID,
		"mode":               c.Mode,
		"target_amount_g":    c.TargetAmountG,
		"pulses_executed":    c.PulsesExecuted,
		"total_amount_g":     c.TotalAmountG,
		"started_at":         c.StartedAt.Format("2006-01-02T15:04:05.999999999Z"),
		"termination_reason": c.TerminationReason,
		"speed_rpm":          c.SpeedRpm,
		"amount":             c.Amount,
		// Phase 5 (load cell). 0 = weight 미수신 (stub fallback). dashboard 는 0 인 경우 "추정만" 표시.
		"actual_total_amount_g": c.ActualTotalAmountG,
		"silo_depletion_warned": c.SiloDepletionWarned,
	}
	if c.CompletedAt != nil {
		v["status"] = "completed"
		v["completed_at"] = c.CompletedAt.Format("2006-01-02T15:04:05.999999999Z")
	} else {
		v["status"] = "active"
	}
	if c.IntentID != "" {
		v["intent_id"] = c.IntentID
		// intent_reason 은 별도 조회 (list view 에서도 N <= 100 이므로 acceptable).
		// 조회 실패는 무시 — operator memo 누락이 cycle 표시를 막아선 안 됨.
		if intent, err := s.store.GetOperatorIntent(r.Context(), c.IntentID); err == nil && intent != nil {
			v["intent_reason"] = intent.Reason
		}
	}
	// C-3l: decision_id echo — dashboard 의 "이의 제기" UI 에서 dispute 첨부용.
	// arbiter_decisions.resulting_cycle_id 로 역조회. 조회 실패는 무시
	// (arbiter 미경유 legacy cycle 은 decision_id 가 없을 수 있음).
	if dec, err := s.store.GetArbiterDecisionByCycleID(r.Context(), c.CycleID); err == nil && dec != nil {
		v["decision_id"] = dec.DecisionID
	}
	return v
}

func (s *Server) feedCycleViews(r *http.Request, cycles []*storage.FeedCycle) []map[string]any {
	out := make([]map[string]any, len(cycles))
	for i, c := range cycles {
		out[i] = s.feedCycleView(r, c)
	}
	return out
}

// --- param parsers -----------------------------------------------------------

func parseAdaptiveParams(params map[string]any) (feed_cycle.AdaptiveParams, error) {
	p := feed_cycle.AdaptiveParams{}
	if params == nil {
		params = map[string]any{}
	}
	if v, ok := params["target_amount_g"]; ok {
		p.TargetAmountG = toFloat64(v)
	}
	if p.TargetAmountG <= 0 {
		return p, errorf("target_amount_g must be > 0")
	}
	if v, ok := params["max_pulses"]; ok {
		p.MaxPulses = int(toFloat64(v))
	}
	if v, ok := params["max_duration_min"]; ok {
		p.MaxDurationMin = int(toFloat64(v))
	}
	if v, ok := params["gap_ms"]; ok {
		p.GapMs = int(toFloat64(v))
	}
	if v, ok := params["pulse_duration_ms"]; ok {
		p.PulseDurationMs = int(toFloat64(v))
	}
	// Phase A — ESP32 모터 출력 (선택, 0/미지정 시 펌웨어 default).
	if v, ok := params["speed_rpm"]; ok {
		p.SpeedRpm = int(toFloat64(v))
		if err := validateSpeedRpm(p.SpeedRpm); err != nil {
			return p, err
		}
	}
	if v, ok := params["amount"]; ok {
		p.Amount = int(toFloat64(v))
		if err := validateAmount(p.Amount); err != nil {
			return p, err
		}
	}
	// Phase 5 — 통 잔량 임계 기반 조기 종료 (load cell 수신 시).
	if v, ok := params["stop_at_remaining_g"]; ok {
		p.StopAtRemainingG = toFloat64(v)
		if p.StopAtRemainingG < 0 {
			return p, errorf("stop_at_remaining_g must be >= 0")
		}
	}
	return p, nil
}

func parseFixedParams(params map[string]any) (feed_cycle.FixedParams, error) {
	p := feed_cycle.FixedParams{}
	if params == nil {
		params = map[string]any{}
	}
	if v, ok := params["pulse_duration_ms"]; ok {
		p.PulseDurationMs = int(toFloat64(v))
	}
	if v, ok := params["gap_ms"]; ok {
		p.GapMs = int(toFloat64(v))
	}
	if v, ok := params["total_pulses"]; ok {
		p.TotalPulses = int(toFloat64(v))
	}
	if p.PulseDurationMs <= 0 {
		return p, errorf("pulse_duration_ms must be > 0")
	}
	if p.GapMs < 0 {
		return p, errorf("gap_ms must be >= 0")
	}
	if p.TotalPulses <= 0 {
		return p, errorf("total_pulses must be > 0")
	}
	if v, ok := params["speed_rpm"]; ok {
		p.SpeedRpm = int(toFloat64(v))
		if err := validateSpeedRpm(p.SpeedRpm); err != nil {
			return p, err
		}
	}
	if v, ok := params["amount"]; ok {
		p.Amount = int(toFloat64(v))
		if err := validateAmount(p.Amount); err != nil {
			return p, err
		}
	}
	return p, nil
}

// validateSpeedRpm — ESP32 펌웨어 SPEED_MIN_RPM=14, SPEED_MAX_RPM=42.
// 0 = 펌웨어 default 사용 (NULL 저장과 동일).
func validateSpeedRpm(rpm int) error {
	if rpm == 0 {
		return nil
	}
	if rpm < 14 || rpm > 42 {
		return errorf("speed_rpm out of range (14~42 or 0 for default)")
	}
	return nil
}

// validateAmount — ESP32 DAC 0~255 (0 = 펌웨어 default).
func validateAmount(amount int) error {
	if amount < 0 || amount > 255 {
		return errorf("amount out of range (0~255)")
	}
	return nil
}

func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func errorf(msg string) error {
	return &apiParamError{msg: msg}
}

type apiParamError struct{ msg string }

func (e *apiParamError) Error() string { return e.msg }
