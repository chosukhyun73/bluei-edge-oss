package plantapi

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

// 가공공장 구성: 공장 정보, 구역, 설비, 공정 센서, 로컬 계정.
// 로컬 계정은 인터넷 없이 현장에서 서명하기 위한 것이다 — PIN 으로 본인 확인한다.
// References: docs/54-haccp-process-flow-ccp-design.md §17.2, §19.4, §21.

const (
	roleAdmin    = "admin"
	roleReviewer = "reviewer"
	roleMonitor  = "monitor"

	pinIterations = 100_000
)

var pinRe = regexp.MustCompile(`^[0-9]{4,8}$`)

// /v1/plant/{site_id}/{profile|zones|equipment|sensors|users|certificates|compliance|forms}
func (s *Server) handlePlantRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/plant/"), "/")
	parts := strings.Split(rest, "/")
	forms := len(parts) >= 2 && parts[1] == "forms"
	if (len(parts) != 2 && !forms) || parts[0] == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/plant/{site_id}/{profile|zones|equipment|sensors|users|certificates|compliance|forms}", "")
		return
	}
	siteID := parts[0]
	siteType, err := s.store.GetSiteType(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if siteType != string(site.TypeProcessing) {
		writeError(w, http.StatusUnprocessableEntity, "NOT_PROCESSING_SITE",
			"가공 사업장(site_type=processing)이 아닙니다", "")
		return
	}

	if forms {
		s.handlePlantForms(w, r, siteID, parts[2:])
		return
	}

	type handlers struct {
		get, write func(http.ResponseWriter, *http.Request, string)
	}
	routes := map[string]struct {
		h           handlers
		writeMethod string
	}{
		"profile":   {handlers{s.handleGetPlantProfile, s.handlePutPlantProfile}, http.MethodPut},
		"zones":     {handlers{s.handleListPlantZones, s.handlePostPlantZone}, http.MethodPost},
		"equipment": {handlers{s.handleListPlantEquipment, s.handlePostPlantEquipment}, http.MethodPost},
		"sensors":   {handlers{s.handleListPlantSensors, s.handlePostPlantSensor}, http.MethodPost},
		"users":     {handlers{s.handleListLocalUsers, s.handlePostLocalUser}, http.MethodPost},

		"certificates": {handlers{s.handleListPlantCertificates, s.handlePostPlantCertificate}, http.MethodPost},
		"compliance":   {handlers{s.handleGetPlantCompliance, s.handlePutPlantCompliance}, http.MethodPut},
		"readings":     {handlers{s.handleListReadings, s.handlePostMock}, http.MethodPost},
	}
	rt, ok := routes[parts[1]]
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown plant resource: "+parts[1], "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		rt.h.get(w, r, siteID)
	case rt.writeMethod:
		rt.h.write(w, r, siteID)
	default:
		w.Header().Set("Allow", "GET, "+rt.writeMethod)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── 공장 정보 ────────────────────────────────────────────────────────────────

type plantProfileRequest struct {
	BusinessNo         string `json:"business_no"`
	LicenseName        string `json:"license_name"`
	LicenseNo          string `json:"license_no"`
	LicenseAuthority   string `json:"license_authority"`
	HACCPCertNo        string `json:"haccp_cert_no"`
	HumanWelfarePolicy string `json:"human_welfare_policy"`
	RetentionYears     int    `json:"retention_years"`
}

func (s *Server) handleGetPlantProfile(w http.ResponseWriter, r *http.Request, siteID string) {
	p, err := s.store.GetPlantProfile(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if p == nil {
		p = &storage.PlantProfile{SiteID: siteID, RetentionYears: 3}
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": plantProfileToMap(p)})
}

func (s *Server) handlePutPlantProfile(w http.ResponseWriter, r *http.Request, siteID string) {
	var req plantProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	switch req.HumanWelfarePolicy {
	case "", "none", "internal_policy", "third_party_audit":
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_HUMAN_WELFARE_POLICY",
			"human_welfare_policy 는 none|internal_policy|third_party_audit 중 하나", "")
		return
	}
	if req.RetentionYears == 0 {
		req.RetentionYears = 3
	}
	if req.RetentionYears < 1 || req.RetentionYears > 20 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_RETENTION", "retention_years 는 1~20", "")
		return
	}
	p := &storage.PlantProfile{
		SiteID: siteID, BusinessNo: strings.TrimSpace(req.BusinessNo),
		LicenseName: strings.TrimSpace(req.LicenseName), LicenseNo: strings.TrimSpace(req.LicenseNo),
		LicenseAuthority: strings.TrimSpace(req.LicenseAuthority), HACCPCertNo: strings.TrimSpace(req.HACCPCertNo),
		HumanWelfarePolicy: req.HumanWelfarePolicy, RetentionYears: req.RetentionYears,
	}
	if err := s.store.UpsertPlantProfile(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profile": plantProfileToMap(p)})
}

func plantProfileToMap(p *storage.PlantProfile) map[string]any {
	// GDST 가공 기록에 필요한 값이 비어 있으면 알려 준다.
	missing := []string{}
	if p.LicenseName == "" || p.LicenseNo == "" || p.LicenseAuthority == "" {
		missing = append(missing, "license")
	}
	if p.HumanWelfarePolicy == "" {
		missing = append(missing, "human_welfare_policy")
	}
	return map[string]any{
		"site_id": p.SiteID, "business_no": p.BusinessNo, "license_name": p.LicenseName,
		"license_no": p.LicenseNo, "license_authority": p.LicenseAuthority, "haccp_cert_no": p.HACCPCertNo,
		"human_welfare_policy": p.HumanWelfarePolicy, "retention_years": p.RetentionYears,
		"missing_for_traceability": missing,
	}
}

// ── 구역·설비·센서 ───────────────────────────────────────────────────────────

func writePlantUpsertError(w http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrPlantIDTaken) {
		writeError(w, http.StatusConflict, "ID_TAKEN", "다른 사업장에서 이미 쓰는 id 입니다", "")
		return
	}
	writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
}

// plantItemID returns the caller's id if it is well formed, a generated one if
// the caller sent none, or "" if the caller's id is malformed.
func plantItemID(given, prefix string) string {
	if given == "" {
		return common.NewID(prefix)
	}
	if !domainIDRe.MatchString(given) {
		return ""
	}
	return given
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

type plantZoneRequest struct {
	ZoneID       string `json:"zone_id"`
	Name         string `json:"name"`
	HygieneClass string `json:"hygiene_class"`
	TempClass    string `json:"temp_class"`
}

func (s *Server) handleListPlantZones(w http.ResponseWriter, r *http.Request, siteID string) {
	zones, err := s.store.ListPlantZones(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(zones))
	for _, z := range zones {
		out = append(out, plantZoneToMap(z))
	}
	writeJSON(w, http.StatusOK, map[string]any{"zones": out, "count": len(out)})
}

func (s *Server) handlePostPlantZone(w http.ResponseWriter, r *http.Request, siteID string) {
	var req plantZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.HygieneClass == "" {
		req.HygieneClass = "general"
	}
	if req.TempClass == "" {
		req.TempClass = "ambient"
	}
	id := plantItemID(req.ZoneID, "zone")
	req.Name = strings.TrimSpace(req.Name)
	if id == "" || req.Name == "" || !oneOf(req.HygieneClass, "general", "clean") ||
		!oneOf(req.TempClass, "ambient", "chilled", "frozen") {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_ZONE",
			"name 필수, hygiene_class 는 general|clean, temp_class 는 ambient|chilled|frozen", "")
		return
	}
	z := &storage.PlantZone{ZoneID: id, SiteID: siteID, Name: req.Name, HygieneClass: req.HygieneClass, TempClass: req.TempClass}
	if err := s.store.UpsertPlantZone(r.Context(), z); err != nil {
		writePlantUpsertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "zone": plantZoneToMap(z)})
}

func plantZoneToMap(z *storage.PlantZone) map[string]any {
	return map[string]any{"zone_id": z.ZoneID, "site_id": z.SiteID, "name": z.Name,
		"hygiene_class": z.HygieneClass, "temp_class": z.TempClass}
}

type plantEquipmentRequest struct {
	EquipmentID string `json:"equipment_id"`
	ZoneID      string `json:"zone_id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
}

func (s *Server) handleListPlantEquipment(w http.ResponseWriter, r *http.Request, siteID string) {
	items, err := s.store.ListPlantEquipment(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, e := range items {
		out = append(out, plantEquipmentToMap(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"equipment": out, "count": len(out)})
}

func (s *Server) handlePostPlantEquipment(w http.ResponseWriter, r *http.Request, siteID string) {
	var req plantEquipmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	id := plantItemID(req.EquipmentID, "equip")
	req.Name = strings.TrimSpace(req.Name)
	if id == "" || req.Name == "" ||
		!oneOf(req.Kind, "cold_room", "freezer", "cooker", "washer", "metal_detector", "scale", "tag_reader", "other") {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_EQUIPMENT",
			"name 필수, kind 는 cold_room|freezer|cooker|washer|metal_detector|scale|tag_reader|other", "")
		return
	}
	refs, err := s.plantRefs(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if req.ZoneID != "" && !refs.Zones[req.ZoneID] {
		writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_ZONE", "등록되지 않은 구역: "+req.ZoneID, "")
		return
	}
	e := &storage.PlantEquipment{EquipmentID: id, SiteID: siteID, ZoneID: req.ZoneID, Kind: req.Kind, Name: req.Name}
	if err := s.store.UpsertPlantEquipment(r.Context(), e); err != nil {
		writePlantUpsertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "equipment": plantEquipmentToMap(e)})
}

func plantEquipmentToMap(e *storage.PlantEquipment) map[string]any {
	return map[string]any{"equipment_id": e.EquipmentID, "site_id": e.SiteID, "zone_id": e.ZoneID,
		"kind": e.Kind, "name": e.Name}
}

type plantSensorRequest struct {
	SensorID         string `json:"sensor_id"`
	ZoneID           string `json:"zone_id"`
	EquipmentID      string `json:"equipment_id"`
	Metric           string `json:"metric"`
	Unit             string `json:"unit"`
	IsMock           bool   `json:"is_mock"`
	CalibrationDueAt string `json:"calibration_due_at"` // RFC3339, 선택
}

func (s *Server) handleListPlantSensors(w http.ResponseWriter, r *http.Request, siteID string) {
	items, err := s.store.ListPlantSensors(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, sn := range items {
		out = append(out, plantSensorToMap(sn))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sensors": out, "count": len(out)})
}

func (s *Server) handlePostPlantSensor(w http.ResponseWriter, r *http.Request, siteID string) {
	var req plantSensorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	id := plantItemID(req.SensorID, "psensor")
	req.Metric = strings.TrimSpace(req.Metric)
	if id == "" || req.Metric == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SENSOR", "metric 필수", "")
		return
	}
	// 센서는 구역이나 설비 중 적어도 한 곳에 붙어야 한다.
	if req.ZoneID == "" && req.EquipmentID == "" {
		writeError(w, http.StatusUnprocessableEntity, "SENSOR_UNBOUND", "zone_id 또는 equipment_id 필수", "")
		return
	}
	refs, err := s.plantRefs(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if req.ZoneID != "" && !refs.Zones[req.ZoneID] {
		writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_ZONE", "등록되지 않은 구역: "+req.ZoneID, "")
		return
	}
	if req.EquipmentID != "" && !refs.Equipment[req.EquipmentID] {
		writeError(w, http.StatusUnprocessableEntity, "UNKNOWN_EQUIPMENT", "등록되지 않은 설비: "+req.EquipmentID, "")
		return
	}
	sn := &storage.PlantSensor{SensorID: id, SiteID: siteID, ZoneID: req.ZoneID, EquipmentID: req.EquipmentID,
		Metric: req.Metric, Unit: strings.TrimSpace(req.Unit), IsMock: req.IsMock}
	if req.CalibrationDueAt != "" {
		t, err := time.Parse(time.RFC3339, req.CalibrationDueAt)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_CALIBRATION_DUE", "calibration_due_at 은 RFC3339", "")
			return
		}
		sn.CalibrationDueAt = &t
	}
	if err := s.store.UpsertPlantSensor(r.Context(), sn); err != nil {
		writePlantUpsertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sensor": plantSensorToMap(sn)})
}

func plantSensorToMap(sn *storage.PlantSensor) map[string]any {
	m := map[string]any{"sensor_id": sn.SensorID, "site_id": sn.SiteID, "zone_id": sn.ZoneID,
		"equipment_id": sn.EquipmentID, "metric": sn.Metric, "unit": sn.Unit, "is_mock": sn.IsMock}
	if sn.CalibrationDueAt != nil {
		m["calibration_due_at"] = common.FormatTime(*sn.CalibrationDueAt)
	}
	return m
}

// plantRefs collects the ids registered at a plant, for reference checks.
func (s *Server) plantRefs(ctx context.Context, siteID string) (haccp.SiteRefs, error) {
	refs := haccp.SiteRefs{Zones: map[string]bool{}, Equipment: map[string]bool{}, Sensors: map[string]bool{}}
	zones, err := s.store.ListPlantZones(ctx, siteID)
	if err != nil {
		return refs, err
	}
	for _, z := range zones {
		refs.Zones[z.ZoneID] = true
	}
	equipment, err := s.store.ListPlantEquipment(ctx, siteID)
	if err != nil {
		return refs, err
	}
	for _, e := range equipment {
		refs.Equipment[e.EquipmentID] = true
	}
	sensors, err := s.store.ListPlantSensors(ctx, siteID)
	if err != nil {
		return refs, err
	}
	for _, sn := range sensors {
		refs.Sensors[sn.SensorID] = true
	}
	return refs, nil
}

// ── 로컬 계정 ────────────────────────────────────────────────────────────────

type localUserRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
	PIN  string `json:"pin"`
}

func (s *Server) handleListLocalUsers(w http.ResponseWriter, r *http.Request, siteID string) {
	users, err := s.store.ListLocalUsers(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, localUserToMap(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "count": len(out)})
}

func (s *Server) handlePostLocalUser(w http.ResponseWriter, r *http.Request, siteID string) {
	var req localUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || !oneOf(req.Role, roleAdmin, roleReviewer, roleMonitor) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_USER", "name 필수, role 은 admin|reviewer|monitor", "")
		return
	}
	if !pinRe.MatchString(req.PIN) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PIN", "PIN 은 숫자 4~8자리", "")
		return
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		writeError(w, http.StatusInternalServerError, "RANDOM_ERROR", err.Error(), "")
		return
	}
	hash, err := hashPIN(req.PIN, salt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HASH_ERROR", err.Error(), "")
		return
	}
	u := &storage.LocalUser{UserID: common.NewID("user"), SiteID: siteID, Name: req.Name, Role: req.Role,
		PinSalt: hex.EncodeToString(salt), PinHash: hash, Active: true}
	if err := s.store.InsertLocalUser(r.Context(), u); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "user": localUserToMap(u)})
}

// localUserToMap never includes the PIN salt or hash.
func localUserToMap(u *storage.LocalUser) map[string]any {
	return map[string]any{"user_id": u.UserID, "site_id": u.SiteID, "name": u.Name, "role": u.Role, "active": u.Active}
}

func hashPIN(pin string, salt []byte) (string, error) {
	key, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}

var (
	errSignerNotFound = errors.New("서명자를 찾을 수 없습니다")
	errSignerRole     = errors.New("이 서명을 할 수 있는 역할이 아닙니다")
	errSignerPIN      = errors.New("PIN 이 맞지 않습니다")
)

// verifySigner checks that userID is an active local user of the site, holds
// one of the allowed roles, and that pin is that user's PIN.
func (s *Server) verifySigner(ctx context.Context, siteID, userID, pin string, roles ...string) (*storage.LocalUser, error) {
	u, err := s.store.GetLocalUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.SiteID != siteID || !u.Active {
		return nil, errSignerNotFound
	}
	if !oneOf(u.Role, roles...) {
		return nil, errSignerRole
	}
	salt, err := hex.DecodeString(u.PinSalt)
	if err != nil {
		return nil, err
	}
	got, err := hashPIN(pin, salt)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(u.PinHash)) != 1 {
		return nil, errSignerPIN
	}
	return u, nil
}
