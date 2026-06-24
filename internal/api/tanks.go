package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/storage"
)

// handleTanksRoute dispatches GET (list) and POST (create) on /v1/tanks.
func (s *Server) handleTanksRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleTanks(w, r)
	case http.MethodPost:
		s.handlePostTank(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTanks(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.ListTankProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if profiles == nil {
		profiles = make([]*storage.TankProfile, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": profiles,
		"count": len(profiles),
	})
}

// tankPostRequest 는 운영자 신규 수조 등록 body.
// storage.TankProfile (= config.TankProfile) 를 그대로 받되 필수 필드만 검증.
type tankPostRequest struct {
	TankID           string                `json:"tank_id"`
	PlatformTankID   string                `json:"platform_tank_id,omitempty"`
	DisplayName      string                `json:"display_name"`
	Species          string                `json:"species"`
	SystemType       string                `json:"system_type"`
	VolumeM3         float64               `json:"volume_m3,omitempty"`
	BiomassKg        float64               `json:"biomass_kg,omitempty"`
	FishCount        int                   `json:"fish_count,omitempty"`
	AvgWeightG       float64               `json:"avg_weight_g,omitempty"`
	TargetRanges     []storage.MetricRange `json:"target_ranges,omitempty"`
	Metadata         map[string]any        `json:"metadata,omitempty"`
	GroupID          string                `json:"group_id,omitempty"`
	SiteID           string                `json:"site_id,omitempty"`
	WTGID            string                `json:"wtg_id,omitempty"`
	LotNo            string                `json:"lot_no,omitempty"`
	LifecycleStage   string                `json:"lifecycle_stage,omitempty"`
	MutableLifecycle bool                  `json:"mutable_lifecycle,omitempty"`
	// C-9 — 물리 정보. 모두 optional (운영자 부분 입력 허용).
	FormFactor string  `json:"form_factor,omitempty"` // 'round' | 'square' | 'rectangular'
	DiameterM  float64 `json:"diameter_m,omitempty"`
	LengthM    float64 `json:"length_m,omitempty"`
	WidthM     float64 `json:"width_m,omitempty"`
	DepthM     float64 `json:"depth_m,omitempty"`
}

func (s *Server) handlePostTank(w http.ResponseWriter, r *http.Request) {
	var req tankPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if !domainIDRe.MatchString(req.TankID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TANK_ID",
			"tank_id is required (소문자/숫자/_/-, 1-64자)", "")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "display_name is required", "")
		return
	}
	if req.Species == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "species is required", "")
		return
	}
	if req.SystemType == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_FIELDS", "system_type is required", "")
		return
	}
	// C-9 — form_factor 일관성 검증 (선택 입력 허용, 입력 시에만 검증).
	switch req.FormFactor {
	case "", "round", "square", "rectangular":
		// ok
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_FORM_FACTOR",
			"form_factor must be one of: round|square|rectangular", "")
		return
	}
	p := &storage.TankProfile{
		TankID:           req.TankID,
		PlatformTankID:   req.PlatformTankID,
		DisplayName:      req.DisplayName,
		Species:          req.Species,
		SystemType:       req.SystemType,
		VolumeM3:         req.VolumeM3,
		BiomassKg:        req.BiomassKg,
		FishCount:        req.FishCount,
		AvgWeightG:       req.AvgWeightG,
		TargetRanges:     req.TargetRanges,
		Metadata:         req.Metadata,
		GroupID:          req.GroupID,
		SiteID:           req.SiteID,
		WTGID:            req.WTGID,
		LotNo:            req.LotNo,
		LifecycleStage:   req.LifecycleStage,
		MutableLifecycle: req.MutableLifecycle,
		FormFactor:       req.FormFactor,
		DiameterM:        req.DiameterM,
		LengthM:          req.LengthM,
		WidthM:           req.WidthM,
		DepthM:           req.DepthM,
	}
	if err := s.store.UpsertTankProfile(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": p})
}

func (s *Server) handleTankProfile(w http.ResponseWriter, r *http.Request) {
	tankID := tankIDFromProfilePath(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/profile", "")
		return
	}
	profile, err := s.store.GetTankProfile(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "tank profile not found", "")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func tankIDFromProfilePath(path string) string {
	if !strings.HasPrefix(path, "/v1/tanks/") || !strings.HasSuffix(path, "/profile") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/tanks/"), "/profile")
	return strings.Trim(id, "/")
}
