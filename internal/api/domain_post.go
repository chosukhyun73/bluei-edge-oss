package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/sensor"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/species"
	"bluei.kr/edge/internal/wtg"
)

// domainIDRe — 도메인 ID 공통 형식 (소문자/숫자/언더스코어/하이픈, 1-64자).
var domainIDRe = regexp.MustCompile(`^[a-z0-9_\-]{1,64}$`)

// ──────────────────────────────────────────────────────────────────────────────
// /v1/farms — GET + POST 디스패치
// ──────────────────────────────────────────────────────────────────────────────

// handleFarmsRoute dispatches GET (list) and POST (create) on /v1/farms.
func (s *Server) handleFarmsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetFarms(w, r)
	case http.MethodPost:
		s.handlePostFarm(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type farmPostRequest struct {
	FarmID         string         `json:"farm_id"`
	LicenseNo      string         `json:"license_no"`
	Operator       string         `json:"operator"`
	Certifications []string       `json:"certifications"`
	Metadata       map[string]any `json:"metadata"`
}

func (s *Server) handlePostFarm(w http.ResponseWriter, r *http.Request) {
	var req farmPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.FarmID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_FARM_ID",
			"farm_id is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.Operator == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "operator is required", "")
		return
	}
	f := &farm.Farm{
		FarmID:         req.FarmID,
		LicenseNo:      req.LicenseNo,
		Operator:       req.Operator,
		Certifications: req.Certifications,
		Metadata:       req.Metadata,
	}
	if err := s.store.UpsertFarm(r.Context(), f); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": f})
}

// ──────────────────────────────────────────────────────────────────────────────
// /v1/sites — GET + POST 디스패치 (site_type 분기)
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSitesRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSites(w, r)
	case http.MethodPost:
		s.handlePostSite(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type sitePostRequest struct {
	SiteID     string         `json:"site_id"`
	FarmID     string         `json:"farm_id"`
	SiteType   string         `json:"site_type"` // 'land' | 'marine'
	Name       string         `json:"name"`
	Timezone   string         `json:"timezone"`
	Address    string         `json:"address,omitempty"`
	Lat        *float64       `json:"lat,omitempty"`
	Lon        *float64       `json:"lon,omitempty"`
	HeadingDeg *float64       `json:"heading_deg,omitempty"`
	Metadata   map[string]any `json:"metadata"`
}

func (s *Server) handlePostSite(w http.ResponseWriter, r *http.Request) {
	var req sitePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.SiteID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SITE_ID",
			"site_id is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.FarmID == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "farm_id is required", "")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "name is required", "")
		return
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Seoul"
	}

	ctx := r.Context()
	switch req.SiteType {
	case "land":
		sl := &site.SiteLand{
			SiteID:   req.SiteID,
			FarmID:   req.FarmID,
			Name:     req.Name,
			Timezone: req.Timezone,
			Metadata: req.Metadata,
			Location: site.LandLocation{Address: req.Address},
		}
		if req.Lat != nil && req.Lon != nil {
			sl.Location.Coordinates = &site.Coordinates{Lat: *req.Lat, Lon: *req.Lon}
		}
		if err := s.store.UpsertSiteLand(ctx, sl); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": sl})
	case "marine":
		sm := &site.SiteMarine{
			SiteID:   req.SiteID,
			FarmID:   req.FarmID,
			Name:     req.Name,
			Timezone: req.Timezone,
			Metadata: req.Metadata,
		}
		if req.HeadingDeg != nil {
			sm.Location.HeadingDeg = *req.HeadingDeg
		}
		if err := s.store.UpsertSiteMarine(ctx, sm); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": sm})
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SITE_TYPE",
			"site_type must be 'land' or 'marine'", "")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// /v1/water-treatment-groups — GET + POST
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleWTGsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetWTGs(w, r)
	case http.MethodPost:
		s.handlePostWTG(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type wtgPostRequest struct {
	WTGID           string              `json:"wtg_id"`
	SiteID          string              `json:"site_id"`
	Name            string              `json:"name"`
	TankIDs         []string            `json:"tank_ids"`
	SharedEquipment wtg.SharedEquipment `json:"shared_equipment"`
	IntakeSensor    string              `json:"intake_sensor"`
	OutletSensor    string              `json:"outlet_sensor"`
	Capacity        wtg.Capacity        `json:"capacity"`
	FeedingPolicy   wtg.FeedingPolicy   `json:"feeding_policy"`
}

func (s *Server) handlePostWTG(w http.ResponseWriter, r *http.Request) {
	var req wtgPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.WTGID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_WTG_ID",
			"wtg_id is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.SiteID == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "site_id is required", "")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "name is required", "")
		return
	}
	g := &wtg.Group{
		WTGID:           req.WTGID,
		SiteID:          req.SiteID,
		Name:            req.Name,
		TankIDs:         req.TankIDs,
		SharedEquipment: req.SharedEquipment,
		IntakeSensor:    req.IntakeSensor,
		OutletSensor:    req.OutletSensor,
		Capacity:        req.Capacity,
		FeedingPolicy:   req.FeedingPolicy,
	}
	if err := s.store.UpsertWTG(r.Context(), g); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": g})
}

// ──────────────────────────────────────────────────────────────────────────────
// /v1/sensors — GET + POST
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSensorsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSensors(w, r)
	case http.MethodPost:
		s.handlePostSensor(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type sensorPostRequest struct {
	SensorID     string         `json:"sensor_id"`
	SensorType   string         `json:"sensor_type"`
	SiteID       string         `json:"site_id"`
	TankID       string         `json:"tank_id"`
	WTGID        string         `json:"wtg_id"`
	Position     string         `json:"position"`
	Hardware     string         `json:"hardware"`
	Capabilities []string       `json:"capabilities"`
	Metadata     map[string]any `json:"metadata"`
	// C-13a — 센서 모델 라이브러리 link + 인스턴스 메타.
	ModelID           string   `json:"model_id"`
	MountLocation     string   `json:"mount_location"`
	InstalledDepthM   *float64 `json:"installed_depth_m,omitempty"`
	MeasurementRole   []string `json:"measurement_role"`
	CalibrationLastAt string   `json:"calibration_last_at"`
	CalibrationDueAt  string   `json:"calibration_due_at"`
}

// C-13a — 센서 인스턴스 enum (모델은 별도 enum — sensor_models.go).
// mount_location : 어디 위에 설치 (tank vs wtg context 모두 수용).
var validSensorMountLocations = map[string]bool{
	"water_intake": true, "water_outlet": true,
	"tank_top": true, "tank_bottom": true, "mid_depth": true,
	"feeder_inline": true, "pipe_inline": true,
	"wtg_intake": true, "wtg_outlet": true,
	"other": true,
}

// measurement_role : 운영자 의도 (다중 선택).
var validSensorMeasurementRoles = map[string]bool{
	"safety_gate_c3":           true,
	"feeding_decision":         true,
	"water_quality_monitoring": true,
	"predictive_input":         true,
	"operator_only":            true,
}

func (s *Server) handlePostSensor(w http.ResponseWriter, r *http.Request) {
	var req sensorPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.SensorID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SENSOR_ID",
			"sensor_id is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.SensorType == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "sensor_type is required", "")
		return
	}
	// C-13a — mount_location / measurement_role enum validation.
	if req.MountLocation != "" && !validSensorMountLocations[req.MountLocation] {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_MOUNT_LOCATION",
			"mount_location 가 허용 enum 이 아닙니다 (water_intake/water_outlet/tank_top/tank_bottom/mid_depth/feeder_inline/pipe_inline/wtg_intake/wtg_outlet/other)", "")
		return
	}
	for _, role := range req.MeasurementRole {
		if !validSensorMeasurementRoles[role] {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_MEASUREMENT_ROLE",
				"measurement_role 항목이 허용 enum 이 아닙니다 (safety_gate_c3/feeding_decision/water_quality_monitoring/predictive_input/operator_only)", "")
			return
		}
	}
	sen := &sensor.Sensor{
		SensorID:          req.SensorID,
		SensorType:        req.SensorType,
		SiteID:            req.SiteID,
		TankID:            req.TankID,
		WTGID:             req.WTGID,
		Position:          req.Position,
		Hardware:          req.Hardware,
		Capabilities:      req.Capabilities,
		Metadata:          req.Metadata,
		ModelID:           req.ModelID,
		MountLocation:     req.MountLocation,
		InstalledDepthM:   req.InstalledDepthM,
		MeasurementRole:   req.MeasurementRole,
		CalibrationLastAt: req.CalibrationLastAt,
		CalibrationDueAt:  req.CalibrationDueAt,
	}
	if err := s.store.UpsertSensor(r.Context(), sen); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": sen})
}

// ──────────────────────────────────────────────────────────────────────────────
// /v1/actuators — GET + POST
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleActuatorsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetActuators(w, r)
	case http.MethodPost:
		s.handlePostActuator(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type actuatorPostRequest struct {
	DeviceID       string                   `json:"device_id"`
	DeviceType     string                   `json:"device_type"`
	SiteID         string                   `json:"site_id"`
	TankID         string                   `json:"tank_id"`
	WTGID          string                   `json:"wtg_id"`
	ControllerID   string                   `json:"controller_id"`
	Model          string                   `json:"model"`
	RatedPowerW    float64                  `json:"rated_power_w"`
	PositionInTank *actuator.PositionInTank `json:"position_in_tank,omitempty"`
	Capabilities   []string                 `json:"capabilities"`
	Metadata       map[string]any           `json:"metadata"`
	// C-13b 액추에이터 모델 라이브러리 + 안전/운영 메타.
	ModelID              string         `json:"model_id,omitempty"`
	MountLocation        string         `json:"mount_location,omitempty"`
	SafetyRoles          []string       `json:"safety_role,omitempty"`
	OperatingMode        string         `json:"operating_mode,omitempty"`
	AlarmThresholds      map[string]any `json:"alarm_thresholds,omitempty"`
	LastMaintenanceAt    string         `json:"last_maintenance_at,omitempty"`
	NextMaintenanceDueAt string         `json:"next_maintenance_due_at,omitempty"`
}

// C-13b 액추에이터 인스턴스 enum (모델은 별도 enum — actuator_models.go).
// mount_location : tank vs wtg context 모두 수용.
var validActuatorMountLocations = map[string]bool{
	"tank_inlet": true, "tank_outlet": true, "tank_center": true, "tank_bottom": true,
	"wtg_intake": true, "wtg_outlet": true,
	"pipe_inline": true, "feeder_zone": true, "external": true, "other": true,
}

// safety_roles : 운영 안전 의도 (다중 선택). Mode Arbiter / C-3 게이트 연결.
var validActuatorSafetyRoles = map[string]bool{
	"oxygen_critical":      true,
	"circulation_critical": true,
	"feed_actuator":        true,
	"filtration":           true,
	"heating_cooling":      true,
	"lighting":             true,
	"oxygen_backup":        true,
	"disinfection":         true,
	"non_critical":         true,
}

var validActuatorOperatingModes = map[string]bool{
	"auto": true, "manual": true, "standby": true, "maintenance": true, "fault": true,
}

func (s *Server) handlePostActuator(w http.ResponseWriter, r *http.Request) {
	var req actuatorPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.DeviceID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DEVICE_ID",
			"device_id is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.DeviceType == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "device_type is required", "")
		return
	}
	// C-13b enum validation — 모르는 값은 422.
	if req.MountLocation != "" && !validActuatorMountLocations[req.MountLocation] {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_MOUNT_LOCATION",
			"mount_location 가 허용 enum 이 아닙니다 (tank_inlet/tank_outlet/tank_center/tank_bottom/wtg_intake/wtg_outlet/pipe_inline/feeder_zone/external/other)", "")
		return
	}
	for _, role := range req.SafetyRoles {
		if !validActuatorSafetyRoles[role] {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_SAFETY_ROLE",
				"safety_role 항목이 허용 enum 이 아닙니다 (oxygen_critical/circulation_critical/feed_actuator/filtration/heating_cooling/lighting/oxygen_backup/disinfection/non_critical): "+role, "")
			return
		}
	}
	if req.OperatingMode != "" && !validActuatorOperatingModes[req.OperatingMode] {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_OPERATING_MODE",
			"operating_mode 가 허용 enum 이 아닙니다 (auto/manual/standby/maintenance/fault)", "")
		return
	}
	a := &actuator.Actuator{
		DeviceID:             req.DeviceID,
		DeviceType:           req.DeviceType,
		SiteID:               req.SiteID,
		TankID:               req.TankID,
		WTGID:                req.WTGID,
		ControllerID:         req.ControllerID,
		Model:                req.Model,
		RatedPowerW:          req.RatedPowerW,
		PositionInTank:       req.PositionInTank,
		Capabilities:         req.Capabilities,
		Metadata:             req.Metadata,
		ModelID:              req.ModelID,
		MountLocation:        req.MountLocation,
		SafetyRoles:          req.SafetyRoles,
		OperatingMode:        req.OperatingMode,
		AlarmThresholds:      req.AlarmThresholds,
		LastMaintenanceAt:    req.LastMaintenanceAt,
		NextMaintenanceDueAt: req.NextMaintenanceDueAt,
	}
	if err := s.store.UpsertActuator(r.Context(), a); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	// 자원 매핑이 곧 컨트롤러↔탱크 연결이 되도록 controller.tank_id 를 동기화한다.
	// command 라우팅(arbiter)·미연결 표시 모두 controller.tank_id 를 source of truth 로 쓰는데,
	// 등록 후 이를 갱신할 다른 경로가 없다. 동기화 실패는 actuator 등록을 무효화하지 않는다.
	if a.ControllerID != "" && a.TankID != "" {
		if err := s.syncControllerTank(r.Context(), a.ControllerID, a.TankID); err != nil {
			slog.Warn("actuator mapping: controller tank sync failed",
				"controller_id", a.ControllerID, "tank_id", a.TankID, "error", err)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": a})
}

// syncControllerTank — actuator 매핑으로 결정된 tank 를 controller.tank_id 에 반영.
func (s *Server) syncControllerTank(ctx context.Context, controllerID, tankID string) error {
	c, err := s.store.GetController(ctx, controllerID)
	if err != nil {
		return err
	}
	if c == nil || c.TankID == tankID {
		return nil
	}
	c.TankID = tankID
	return s.store.UpsertController(ctx, c)
}

// ──────────────────────────────────────────────────────────────────────────────
// /v1/species-profiles — GET + POST
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleSpeciesProfilesRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSpeciesProfiles(w, r)
	case http.MethodPost:
		s.handlePostSpeciesProfile(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type speciesPostRequest struct {
	Species         string                            `json:"species"`
	DisplayName     string                            `json:"display_name"`
	LifecycleStages map[string]species.LifecycleStage `json:"lifecycle_stages"`
	WasteModel      species.WasteModel                `json:"waste_model"`
	Source          string                            `json:"source"`
}

func (s *Server) handlePostSpeciesProfile(w http.ResponseWriter, r *http.Request) {
	var req speciesPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.Species) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SPECIES_KEY",
			"species is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "display_name is required", "")
		return
	}
	src := req.Source
	if src == "" {
		src = "override"
	}
	p := &species.Profile{
		DisplayName:     req.DisplayName,
		LifecycleStages: req.LifecycleStages,
		WasteModel:      req.WasteModel,
		Source:          src,
	}
	if err := s.store.UpsertSpeciesProfile(r.Context(), req.Species, p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":      true,
		"species": req.Species,
		"item":    p,
	})
}
