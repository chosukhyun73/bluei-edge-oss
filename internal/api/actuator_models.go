package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/storage"
)

// C-13b actuator_models — 액추에이터 모델 라이브러리 API.
// GET    /v1/actuator-models         — list
// POST   /v1/actuator-models         — upsert (model_id 필수)
// DELETE /v1/actuator-models/{id}    — delete (자식 인스턴스 있으면 409)

type actuatorModelRequest struct {
	ModelID                   string   `json:"model_id"`
	Vendor                    string   `json:"vendor"`
	ProductCode               string   `json:"product_code"`
	DisplayName               string   `json:"display_name"`
	DeviceCategory            string   `json:"device_category"`
	RatedPowerW               *float64 `json:"rated_power_w,omitempty"`
	CapacityValue             *float64 `json:"capacity_value,omitempty"`
	CapacityUnit              string   `json:"capacity_unit,omitempty"`
	ControlMethod             string   `json:"control_method,omitempty"`
	ResponseTimeS             *float64 `json:"response_time_s,omitempty"`
	ControlRangeMin           *float64 `json:"control_range_min,omitempty"`
	ControlRangeMax           *float64 `json:"control_range_max,omitempty"`
	ControlRangeUnit          string   `json:"control_range_unit,omitempty"`
	ConsumableReplacementDays *int     `json:"consumable_replacement_days,omitempty"`
	Notes                     string   `json:"notes,omitempty"`
	// CategorySpecs — 카테고리별 spec (공통 컬럼으로 표현 안 되는 고유 spec).
	// 키는 카테고리별로 다르며, 일부 카테고리는 필수 키 검증을 받는다.
	CategorySpecs map[string]any `json:"category_specs,omitempty"`
}

// 마이그 CHECK 와 동일한 enum.
var validActuatorDeviceCategories = map[string]bool{
	"pump": true, "aerator": true, "oxygen_cone": true, "heater": true,
	"chiller": true, "uv_sterilizer": true, "led_light": true, "feeder": true,
	"valve": true, "biofilter": true, "drum_filter": true, "dosing_pump": true,
	"ozonator": true, "blower": true, "skimmer": true, "other": true,
	"circulation_pump": true, "heat_pump": true, "air_pump": true,
}

// actuatorCategoryRequiredSpecs — 카테고리별 category_specs 필수 키.
// 등록 docs/wip/equipment-category-spec-design.md 의 필수 spec 기준.
// 여기 없는 카테고리(pump 등 기존)는 category_specs 없이도 통과 (하위호환).
var actuatorCategoryRequiredSpecs = map[string][]string{
	"circulation_pump": {"max_head_m", "max_flow_m3h"},
	"heat_pump":        {"cooling_capacity_kcal_h"},
	"uv_sterilizer":    {"lamp_wavelength_nm", "lamp_power_w", "lamp_count", "treatment_flow_ton_h"},
	// feeder: 공급속도(rpm)·살포거리는 측정 불가한 무의미 스펙이라 필수 없음.
}

var validActuatorControlMethods = map[string]bool{
	"on_off": true, "pwm": true, "4-20ma": true, "0-10v": true,
	"modbus": true, "mqtt": true, "esp32_controller": true, "manual": true, "other": true,
}

func (s *Server) handleActuatorModelsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListActuatorModels(w, r)
	case http.MethodPost:
		s.handleUpsertActuatorModel(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleActuatorModelItem(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/actuator-models/"), "/")
	if rel == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/actuator-models/{model_id}", "")
		return
	}
	modelID := rel
	switch r.Method {
	case http.MethodGet:
		m, err := s.store.GetActuatorModel(r.Context(), modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if m == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "actuator model not found", "")
			return
		}
		writeJSON(w, http.StatusOK, m)
	case http.MethodDelete:
		// FK 보호 — 자식 액추에이터 인스턴스 있으면 reject.
		n, err := s.store.CountActuatorsForModel(r.Context(), modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, "ACTUATOR_MODEL_IN_USE",
				"이 모델을 사용하는 액추에이터 인스턴스가 있습니다. 먼저 인스턴스를 삭제하세요.", "")
			return
		}
		if err := s.store.DeleteActuatorModel(r.Context(), modelID); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": modelID})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListActuatorModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListActuatorModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	items := make([]any, 0, len(models))
	for _, m := range models {
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleUpsertActuatorModel(w http.ResponseWriter, r *http.Request) {
	var req actuatorModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	model, err := actuatorModelFromRequest(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_ACTUATOR_MODEL", err.Error(), "")
		return
	}
	if err := s.store.UpsertActuatorModel(r.Context(), model); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": model})
}

func actuatorModelFromRequest(req actuatorModelRequest) (*storage.ActuatorModel, error) {
	if strings.TrimSpace(req.ModelID) == "" {
		return nil, errors.New("model_id is required")
	}
	if strings.TrimSpace(req.Vendor) == "" {
		return nil, errors.New("vendor is required")
	}
	if strings.TrimSpace(req.ProductCode) == "" {
		return nil, errors.New("product_code is required")
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		return nil, errors.New("display_name is required")
	}
	if !validActuatorDeviceCategories[req.DeviceCategory] {
		return nil, errors.New("device_category must be one of: pump,aerator,oxygen_cone,heater,chiller,uv_sterilizer,led_light,feeder,valve,biofilter,drum_filter,dosing_pump,ozonator,blower,skimmer,other")
	}
	if req.ControlMethod != "" && !validActuatorControlMethods[req.ControlMethod] {
		return nil, errors.New("control_method must be one of: on_off,pwm,4-20ma,0-10v,modbus,mqtt,esp32_controller,manual,other")
	}
	if err := validateActuatorCategorySpecs(req.DeviceCategory, req.CategorySpecs); err != nil {
		return nil, err
	}
	// category_specs 를 JSON 문자열로 직렬화해 저장. 없으면 빈 문자열.
	specsJSON := ""
	if len(req.CategorySpecs) > 0 {
		b, err := json.Marshal(req.CategorySpecs)
		if err != nil {
			return nil, errors.New("category_specs is not serializable JSON")
		}
		specsJSON = string(b)
	}
	return &storage.ActuatorModel{
		ModelID:                   req.ModelID,
		Vendor:                    req.Vendor,
		ProductCode:               req.ProductCode,
		DisplayName:               req.DisplayName,
		DeviceCategory:            req.DeviceCategory,
		RatedPowerW:               req.RatedPowerW,
		CapacityValue:             req.CapacityValue,
		CapacityUnit:              req.CapacityUnit,
		ControlMethod:             req.ControlMethod,
		ResponseTimeS:             req.ResponseTimeS,
		ControlRangeMin:           req.ControlRangeMin,
		ControlRangeMax:           req.ControlRangeMax,
		ControlRangeUnit:          req.ControlRangeUnit,
		ConsumableReplacementDays: req.ConsumableReplacementDays,
		Notes:                     req.Notes,
		CategorySpecs:             specsJSON,
	}, nil
}

// validateActuatorCategorySpecs — 카테고리별 category_specs 필수 키 검증.
// 필수 키 누락 시 명확한 에러. 기존 카테고리(pump 등)는 필수 spec 없음 → 통과 (하위호환).
func validateActuatorCategorySpecs(category string, specs map[string]any) error {
	// air_pump 는 air_flow 단위(m3min/lpm) 중 하나 + air_pressure_kpa 필수.
	if category == "air_pump" {
		if !specPresent(specs, "air_flow_m3min") && !specPresent(specs, "air_flow_lpm") {
			return errors.New("air_pump category_specs requires air flow: air_flow_m3min or air_flow_lpm")
		}
		if !specPresent(specs, "air_pressure_kpa") {
			return errors.New("air_pump category_specs requires: air_pressure_kpa")
		}
		return nil
	}
	required, ok := actuatorCategoryRequiredSpecs[category]
	if !ok {
		return nil
	}
	missing := []string{}
	for _, key := range required {
		if !specPresent(specs, key) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return errors.New(category + " category_specs requires: " + strings.Join(missing, ", "))
	}
	return nil
}

// specPresent — category_specs 에 key 가 존재하고 nil/빈 문자열이 아닌지.
func specPresent(specs map[string]any, key string) bool {
	v, ok := specs[key]
	if !ok || v == nil {
		return false
	}
	if str, isStr := v.(string); isStr {
		return strings.TrimSpace(str) != ""
	}
	return true
}
