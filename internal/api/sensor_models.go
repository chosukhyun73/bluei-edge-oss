package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/storage"
)

// C-13a sensor_models — 센서 모델 라이브러리 API.
// GET    /v1/sensor-models         — list
// POST   /v1/sensor-models         — upsert (model_id 필수)
// DELETE /v1/sensor-models/{id}    — delete (자식 인스턴스 있으면 409)

type sensorModelRequest struct {
	ModelID                 string   `json:"model_id"`
	Vendor                  string   `json:"vendor"`
	ProductCode             string   `json:"product_code"`
	DisplayName             string   `json:"display_name"`
	MeasurementType         string   `json:"measurement_type"`
	Unit                    string   `json:"unit"`
	RangeMin                *float64 `json:"range_min,omitempty"`
	RangeMax                *float64 `json:"range_max,omitempty"`
	AccuracyValue           *float64 `json:"accuracy_value,omitempty"`
	AccuracyUnit            string   `json:"accuracy_unit,omitempty"`
	ResponseTimeS           *float64 `json:"response_time_s,omitempty"`
	Protocol                string   `json:"protocol,omitempty"`
	CalibrationIntervalDays *int     `json:"calibration_interval_days,omitempty"`
	WetDry                  string   `json:"wet_dry,omitempty"`
	Notes                   string   `json:"notes,omitempty"`
}

// 마이그 024 CHECK 와 동일 enum. 422 응답에서 한국어 메시지로 안내.
var validSensorMeasurementTypes = map[string]bool{
	"water_temperature": true, "ph": true, "dissolved_oxygen": true, "unionized_ammonia": true,
	"nitrate": true, "nitrite": true, "carbon_dioxide": true, "total_suspended_solids": true,
	"turbidity": true, "salinity": true, "flow_rate": true, "pump_pressure": true,
	"water_level": true, "light_intensity": true, "feed_weight": true,
	"oxygen_saturation": true, "redox": true, "conductivity": true, "multi": true, "other": true,
}

var validSensorProtocols = map[string]bool{
	"modbus": true, "rs485": true, "rs232": true, "4-20ma": true, "0-10v": true,
	"i2c": true, "sdi-12": true, "http": true, "mqtt": true, "other": true,
}

var validSensorWetDry = map[string]bool{
	"wet_probe": true, "inline": true, "dry_mount": true, "other": true,
}

func (s *Server) handleSensorModelsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSensorModels(w, r)
	case http.MethodPost:
		s.handleUpsertSensorModel(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSensorModelItem(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/sensor-models/"), "/")
	if rel == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/sensor-models/{model_id}", "")
		return
	}
	modelID := rel
	switch r.Method {
	case http.MethodGet:
		m, err := s.store.GetSensorModel(r.Context(), modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if m == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "sensor model not found", "")
			return
		}
		writeJSON(w, http.StatusOK, m)
	case http.MethodDelete:
		// FK 보호 — 자식 센서 인스턴스 있으면 reject.
		n, err := s.store.CountSensorsForModel(r.Context(), modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, "SENSOR_MODEL_IN_USE",
				"이 모델을 사용하는 센서 인스턴스가 있습니다. 먼저 인스턴스를 삭제하세요.", "")
			return
		}
		if err := s.store.DeleteSensorModel(r.Context(), modelID); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": modelID})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListSensorModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListSensorModels(r.Context())
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

func (s *Server) handleUpsertSensorModel(w http.ResponseWriter, r *http.Request) {
	var req sensorModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	model, err := sensorModelFromRequest(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SENSOR_MODEL", err.Error(), "")
		return
	}
	if err := s.store.UpsertSensorModel(r.Context(), model); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": model})
}

func sensorModelFromRequest(req sensorModelRequest) (*storage.SensorModel, error) {
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
	if !validSensorMeasurementTypes[req.MeasurementType] {
		return nil, errors.New("measurement_type 가 허용 enum 이 아닙니다 (water_temperature/ph/dissolved_oxygen/... 참조)")
	}
	if strings.TrimSpace(req.Unit) == "" {
		return nil, errors.New("unit is required")
	}
	if req.Protocol != "" && !validSensorProtocols[req.Protocol] {
		return nil, errors.New("protocol 가 허용 enum 이 아닙니다 (modbus/rs485/rs232/4-20ma/0-10v/i2c/sdi-12/http/mqtt/other)")
	}
	if req.WetDry != "" && !validSensorWetDry[req.WetDry] {
		return nil, errors.New("wet_dry 가 허용 enum 이 아닙니다 (wet_probe/inline/dry_mount/other)")
	}
	return &storage.SensorModel{
		ModelID:                 req.ModelID,
		Vendor:                  req.Vendor,
		ProductCode:             req.ProductCode,
		DisplayName:             req.DisplayName,
		MeasurementType:         req.MeasurementType,
		Unit:                    req.Unit,
		RangeMin:                req.RangeMin,
		RangeMax:                req.RangeMax,
		AccuracyValue:           req.AccuracyValue,
		AccuracyUnit:            req.AccuracyUnit,
		ResponseTimeS:           req.ResponseTimeS,
		Protocol:                req.Protocol,
		CalibrationIntervalDays: req.CalibrationIntervalDays,
		WetDry:                  req.WetDry,
		Notes:                   req.Notes,
	}, nil
}
