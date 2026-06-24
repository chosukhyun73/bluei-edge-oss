package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

// GET  /v1/feeding-schedules
// POST /v1/feeding-schedules
func (s *Server) handleFeedingSchedulesRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListFeedingSchedules(w, r)
	case http.MethodPost:
		s.handleCreateFeedingSchedule(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET  /v1/feeding-schedules/{id}
// PUT  /v1/feeding-schedules/{id}
// DELETE /v1/feeding-schedules/{id}
// POST /v1/feeding-schedules/{id}/enable
// POST /v1/feeding-schedules/{id}/disable
func (s *Server) handleFeedingScheduleRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/feeding-schedules/")

	if strings.HasSuffix(path, "/enable") {
		id := strings.TrimSuffix(path, "/enable")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleSetScheduleEnabled(w, r, id, true)
		return
	}
	if strings.HasSuffix(path, "/disable") {
		id := strings.TrimSuffix(path, "/disable")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleSetScheduleEnabled(w, r, id, false)
		return
	}

	// plain {id} routes
	switch r.Method {
	case http.MethodGet:
		s.handleGetFeedingSchedule(w, r, path)
	case http.MethodPut:
		s.handleUpdateFeedingSchedule(w, r, path)
	case http.MethodDelete:
		s.handleDeleteFeedingSchedule(w, r, path)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- handler implementations ------------------------------------------------

func (s *Server) handleListFeedingSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.store.ListAllSchedules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if schedules == nil {
		schedules = []*storage.FeedingSchedule{}
	}
	items := make([]map[string]any, len(schedules))
	for i, sc := range schedules {
		items[i] = scheduleView(sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type scheduleRequest struct {
	TankIDs   []string            `json:"tank_ids"`
	Times     []string            `json:"times"`
	Cron      string              `json:"cron"`
	Pattern   schedulePatternBody `json:"pattern"`
	Priority  string              `json:"priority"`
	Enabled   *bool               `json:"enabled"`
	CreatedBy string              `json:"created_by"`
}

type schedulePatternBody struct {
	PulseDurationMs int      `json:"pulse_duration_ms"`
	GapMs           int      `json:"gap_ms"`
	TotalPulses     int      `json:"total_pulses"`
	TargetAmountG   *float64 `json:"target_amount_g"`
}

func (s *Server) handleCreateFeedingSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if errMsg := validateScheduleRequest(&req); errMsg != "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SCHEDULE", errMsg, "")
		return
	}

	sched := scheduleFromRequest(common.NewID("sched"), &req)
	if err := s.store.UpsertSchedule(r.Context(), sched); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"schedule": scheduleView(sched)})
}

func (s *Server) handleGetFeedingSchedule(w http.ResponseWriter, r *http.Request, id string) {
	sched, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if sched == nil {
		writeError(w, http.StatusNotFound, "SCHEDULE_NOT_FOUND", "schedule not found: "+id, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedule": scheduleView(sched)})
}

func (s *Server) handleUpdateFeedingSchedule(w http.ResponseWriter, r *http.Request, id string) {
	existing, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "SCHEDULE_NOT_FOUND", "schedule not found: "+id, "")
		return
	}

	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if errMsg := validateScheduleRequest(&req); errMsg != "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SCHEDULE", errMsg, "")
		return
	}

	sched := scheduleFromRequest(id, &req)
	sched.CreatedAt = existing.CreatedAt
	sched.CreatedBy = existing.CreatedBy
	if err := s.store.UpsertSchedule(r.Context(), sched); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedule": scheduleView(sched)})
}

func (s *Server) handleDeleteFeedingSchedule(w http.ResponseWriter, r *http.Request, id string) {
	existing, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "SCHEDULE_NOT_FOUND", "schedule not found: "+id, "")
		return
	}
	if err := s.store.DeleteSchedule(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetScheduleEnabled(w http.ResponseWriter, r *http.Request, id string, enabled bool) {
	existing, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "SCHEDULE_NOT_FOUND", "schedule not found: "+id, "")
		return
	}
	if err := s.store.SetScheduleEnabled(r.Context(), id, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	existing.Enabled = enabled
	existing.UpdatedAt = time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]any{"schedule": scheduleView(existing)})
}

// --- helpers ----------------------------------------------------------------

func validateScheduleRequest(req *scheduleRequest) string {
	if len(req.TankIDs) == 0 {
		return "tank_ids must be non-empty"
	}
	if len(req.Times) == 0 && req.Cron == "" {
		return "must provide either times (>= 1 entry) or cron"
	}
	if req.Pattern.PulseDurationMs <= 0 {
		return "pattern.pulse_duration_ms must be > 0"
	}
	if req.Pattern.TotalPulses <= 0 {
		return "pattern.total_pulses must be > 0"
	}
	return ""
}

func scheduleFromRequest(id string, req *scheduleRequest) *storage.FeedingSchedule {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	priority := req.Priority
	if priority == "" {
		priority = "manual_override"
	}
	pattern := storage.FeedingSchedulePattern{
		PulseDurationMs: req.Pattern.PulseDurationMs,
		GapMs:           req.Pattern.GapMs,
		TotalPulses:     req.Pattern.TotalPulses,
		TargetAmountG:   req.Pattern.TargetAmountG,
	}
	return &storage.FeedingSchedule{
		ScheduleID: id,
		TankIDs:    req.TankIDs,
		Cron:       req.Cron,
		Times:      req.Times,
		Pattern:    pattern,
		Priority:   priority,
		SafetyGate: true,
		Enabled:    enabled,
		CreatedBy:  req.CreatedBy,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
}

func scheduleView(sc *storage.FeedingSchedule) map[string]any {
	pattern := map[string]any{
		"pulse_duration_ms": sc.Pattern.PulseDurationMs,
		"gap_ms":            sc.Pattern.GapMs,
		"total_pulses":      sc.Pattern.TotalPulses,
	}
	if sc.Pattern.TargetAmountG != nil {
		pattern["target_amount_g"] = *sc.Pattern.TargetAmountG
	}
	v := map[string]any{
		"schedule_id": sc.ScheduleID,
		"tank_ids":    sc.TankIDs,
		"cron":        sc.Cron,
		"times":       sc.Times,
		"pattern":     pattern,
		"priority":    sc.Priority,
		"safety_gate": sc.SafetyGate,
		"enabled":     sc.Enabled,
		"created_by":  sc.CreatedBy,
		"created_at":  sc.CreatedAt.Format(time.RFC3339),
		"updated_at":  sc.UpdatedAt.Format(time.RFC3339),
	}
	return v
}
