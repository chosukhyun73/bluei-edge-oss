package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const setupDraftKVKey = "field_setup_draft"

func (s *Server) handleSetupDraftRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSetupDraft(w, r)
	case http.MethodPost:
		s.handlePostSetupDraft(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSetupDraftValidate(w http.ResponseWriter, r *http.Request) {
	draft, ok := s.loadSetupDraftOrError(w, r)
	if !ok {
		return
	}
	result := validateSetupDraft(draft)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSetupDraftPreview(w http.ResponseWriter, r *http.Request) {
	draft, ok := s.loadSetupDraftOrError(w, r)
	if !ok {
		return
	}
	result := validateSetupDraft(draft)
	writeJSON(w, http.StatusOK, map[string]any{
		"validation": result,
		"files":      buildSetupConfigPreview(draft),
		"apply_mode": "preview_only",
		"note":       "Preview only. Live config files are not modified by this endpoint.",
	})
}

func (s *Server) handleGetSetupDraft(w http.ResponseWriter, r *http.Request) {
	val, ok, err := s.store.KVGet(r.Context(), setupDraftKVKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if !ok || val == "" {
		writeJSON(w, http.StatusOK, map[string]any{"draft": map[string]any{}, "exists": false})
		return
	}
	var draft map[string]any
	if err := json.Unmarshal([]byte(val), &draft); err != nil {
		writeError(w, http.StatusInternalServerError, "DRAFT_DECODE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "exists": true})
}

func (s *Server) handlePostSetupDraft(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "failed to read body", "")
		return
	}
	var draft map[string]any
	if err := json.Unmarshal(body, &draft); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	canonical, _ := json.MarshalIndent(draft, "", "  ")
	if err := s.store.KVSet(r.Context(), setupDraftKVKey, string(canonical)); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "draft": draft})
}

func (s *Server) loadSetupDraftOrError(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	val, ok, err := s.store.KVGet(r.Context(), setupDraftKVKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return nil, false
	}
	if !ok || val == "" {
		writeError(w, http.StatusNotFound, "SETUP_DRAFT_NOT_FOUND", "setup draft not found", "")
		return nil, false
	}
	var draft map[string]any
	if err := json.Unmarshal([]byte(val), &draft); err != nil {
		writeError(w, http.StatusInternalServerError, "DRAFT_DECODE_ERROR", err.Error(), "")
		return nil, false
	}
	return draft, true
}

type setupValidationResult struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

func validateSetupDraft(draft map[string]any) setupValidationResult {
	var errors []string
	var warnings []string
	required := map[string]string{
		"site.site_id":                       "site_id is required",
		"edge.edge_id":                       "edge_id is required",
		"tank.tank_id":                       "tank_id is required",
		"tank.species":                       "species is required",
		"water_quality_sensor.device_id":     "water quality sensor device_id is required",
		"water_quality_sensor.connection":    "water quality sensor connection is required",
		"vision.camera_id":                   "camera_id is required for vision setup",
		"vision.applied_vision_algorithm_id": "vision algorithm selection is required",
		"feeder.feeder_id":                   "feeder_id is required",
		"safety_gate.operator_approval":      "operator approval policy is required",
	}
	for path, msg := range required {
		if strings.TrimSpace(strAt(draft, path)) == "" {
			errors = append(errors, msg)
		}
	}
	if strings.TrimSpace(strAt(draft, "water_quality_sensor.register_map_note")) == "" {
		warnings = append(warnings, "Modbus register map is not entered yet; field connection remains blocked")
	}
	if strings.TrimSpace(strAt(draft, "vision.rtsp_url")) == "" {
		warnings = append(warnings, "RTSP URL is empty; camera validation cannot run yet")
	}
	if strings.TrimSpace(strAt(draft, "water_quality_sensor.serial_port")) == "" {
		warnings = append(warnings, "serial_port is empty; default cannot be assumed safely")
	}
	return setupValidationResult{OK: len(errors) == 0, Errors: errors, Warnings: warnings}
}

func buildSetupConfigPreview(draft map[string]any) map[string]string {
	tankID := strAt(draft, "tank.tank_id")
	if tankID == "" {
		tankID = "tank_01"
	}
	return map[string]string{
		"configs/tanks.generated.yaml": fmt.Sprintf(`tanks:
  - tank_id: %s
    display_name: %s
    species: %s
    system_type: %s
    volume_m3: %s
    fish_count: %s
    avg_weight_g: %s
`, tankID, q(strAt(draft, "tank.display_name")), q(strAt(draft, "tank.species")), q(strAt(draft, "tank.tank_shape")), scalarAt(draft, "tank.volume_m3"), scalarAt(draft, "tank.fish_count"), scalarAt(draft, "tank.avg_weight_g")),
		"configs/vision-algorithms.application.generated.yaml": fmt.Sprintf(`tank_applications:
  - tank_id: %s
    camera_id: %s
    applied_vision_algorithm_id: %s
    current_growth_stage: %s
    current_avg_weight_g: %s
    current_density_range: %s
`, tankID, q(strAt(draft, "vision.camera_id")), q(strAt(draft, "vision.applied_vision_algorithm_id")), q(strAt(draft, "tank.growth_stage")), scalarAt(draft, "tank.avg_weight_g"), q(strAt(draft, "tank.density_range"))),
		"configs/field-sensors.generated.yaml": fmt.Sprintf(`water_quality_sensors:
  - device_id: %s
    tank_id: %s
    connection: %s
    serial_port: %s
    baud_rate: %s
    parity: %s
    stop_bit: %s
`, q(strAt(draft, "water_quality_sensor.device_id")), tankID, q(strAt(draft, "water_quality_sensor.connection")), q(strAt(draft, "water_quality_sensor.serial_port")), q(strAt(draft, "water_quality_sensor.baud_rate")), q(strAt(draft, "water_quality_sensor.parity")), q(strAt(draft, "water_quality_sensor.stop_bit"))),
	}
}

func strAt(m map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

func scalarAt(m map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return "null"
		}
		cur = mm[p]
	}
	if cur == nil {
		return "null"
	}
	return fmt.Sprintf("%v", cur)
}

func q(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}
