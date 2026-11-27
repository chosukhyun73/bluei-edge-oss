package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
		"items": s.withWeightEstimate(r, profiles),
		"count": len(profiles),
	})
}

// withWeightEstimate — 수조 목록에 **추정 체중**을 함께 실어준다.
//
// profile.avg_weight_g 는 마지막 실측 샘플링(앵커)이라 샘플링을 안 하면 몇 달이고
// 그대로다 — 2026-09-07 실측에서 tank_01 이 66일째 857.1g 고정이었고, 화면의 평균
// 체중이 안 변한다는 지적이 여기서 나왔다. 급이량 기반 추정치는 매일
// tank_weight_history 에 쌓이는데(같은 날 1218.5g) 화면까지 오지 않았다.
//
// 둘 다 보여주기로 했다(운영자 결정) — 어느 쪽이 실측이고 어느 쪽이 추정인지,
// 샘플링이 얼마나 밀렸는지 화면에서 드러나야 한다.
//
// 조회 실패는 무시한다. 추정치가 없다고 목록 자체가 실패하면 안 된다.
func (s *Server) withWeightEstimate(r *http.Request, profiles []*storage.TankProfile) []map[string]any {
	out := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		item := map[string]any{}
		if b, err := json.Marshal(p); err == nil {
			_ = json.Unmarshal(b, &item)
		}
		if snaps, err := s.store.ListTankWeightHistory(r.Context(), p.TankID, 1); err == nil && len(snaps) > 0 {
			latest := snaps[0]
			for _, sn := range snaps { // 최신 스냅샷 선택 (정렬 가정하지 않는다)
				if sn.SnapshotAt.After(latest.SnapshotAt) {
					latest = sn
				}
			}
			item["estimated_avg_weight_g"] = latest.EstimatedAvgWeightG
			item["weight_anchor_g"] = latest.AnchorWeightG
			item["weight_anchor_source"] = latest.AnchorSource
			item["weight_days_since_anchor"] = latest.DaysSinceAnchor
			item["weight_quality"] = latest.Quality
		}
		out = append(out, item)
	}
	return out
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
	var beforeMeta map[string]any
	if prev, _ := s.store.GetTankProfile(r.Context(), p.TankID); prev != nil {
		beforeMeta = prev.Metadata
	}
	if err := s.store.UpsertTankProfile(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	s.logTankPolicyOverrideChange(r.Context(), p.TankID, beforeMeta, p.Metadata)
	// 2026-07-28 — 대시보드의 급이 모드 전환(feeding_policy_override.operating_mode)이
	// 이 경로로 저장되는데 스케줄 재생성 hook 이 없어, 다음 5분 폴링까지 반영이 늦었다.
	// 그 지연 때문에 하루에 두 번(10:00 tank_02·15:30 tank_01) 급이 슬롯이 통째로
	// 유실됐다(폴링이 슬롯보다 35초 늦게 도착 → 그 분(HH:MM) 매칭을 놓침).
	// 프로필 저장 즉시 재생성해 전환이 곧바로 반영되게 한다.
	if s.aiScheduler != nil {
		go func(tankID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.aiScheduler.RebuildScheduleForTank(ctx, tankID); err != nil {
				slog.Warn("ai_scheduler rebuild after tank upsert failed", "tank_id", tankID, "error", err)
			}
		}(p.TankID)
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
