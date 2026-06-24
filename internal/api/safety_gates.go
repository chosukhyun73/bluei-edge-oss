package api

import (
	"context"
	"fmt"
	"net/http"

	"bluei.kr/edge/internal/predictive"
)

// handleGetSafetyGatesStatus returns the aggregated activation status of
// the three Phase 4 safety gates for a tank — predictive (C-3p),
// learned (C-3l) and environmental (C-3w).
//
// C-5 (본선 5/27 시연용): 운영자가 신규 사이클 시작 시 "지금 어떤 안전 규칙이
// 살아 있나" 한눈에 보고 의사결정.
//
// GET /v1/safety-gates/status?tank_id=...
//
// 응답:
//
//	{
//	  "tank_id": "...",
//	  "site_id": "...",
//	  "wtg_id":  "...",
//	  "site_type": "land" | "marine",
//	  "predictive":    { "status": "ok|caution|breach|na", "headroom_kg_per_h": .., "summary": "..." },
//	  "learned":       { "status": "ok|active", "rules_enabled": <int>, "summary": "..." },
//	  "environmental": { "status": "ok|caution|breach|na", "summary": "..." }
//	}
//
// status="na" 는 게이트가 해당 tank/site 에 적용되지 않는 경우 (예: land RAS 의 environmental).
func (s *Server) handleGetSafetyGatesStatus(w http.ResponseWriter, r *http.Request) {
	tankID := r.URL.Query().Get("tank_id")
	if tankID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_TANK_ID", "tank_id required", "")
		return
	}

	ctx := r.Context()

	// Tank → site/wtg 매핑
	profile, err := s.store.GetTankProfile(ctx, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "tank not found", "")
		return
	}

	siteType := "land"
	if profile.SiteID != "" {
		sites, err := s.store.ListSites(ctx, "")
		if err == nil {
			for _, st := range sites {
				if st["site_id"] == profile.SiteID {
					if t, _ := st["site_type"].(string); t != "" {
						siteType = t
					}
					break
				}
			}
		}
	}

	resp := map[string]any{
		"tank_id":   tankID,
		"site_id":   profile.SiteID,
		"wtg_id":    profile.WTGID,
		"site_type": siteType,
	}

	resp["predictive"] = s.safetyGatePredictive(ctx, profile.WTGID)
	resp["learned"] = s.safetyGateLearned(ctx)
	resp["environmental"] = s.safetyGateEnvironmental(ctx, siteType, profile.SiteID)

	writeJSON(w, http.StatusOK, resp)
}

// safetyGatePredictive returns the C-3p predictive (NH3 headroom) gate state.
func (s *Server) safetyGatePredictive(ctx context.Context, wtgID string) map[string]any {
	if wtgID == "" {
		return map[string]any{"status": "na", "summary": "WTG 미연결"}
	}
	groups, err := s.store.ListWTGs(ctx, "")
	if err != nil {
		return map[string]any{"status": "na", "summary": "조회 실패"}
	}
	for _, g := range groups {
		if g.WTGID != wtgID {
			continue
		}
		hr, err := predictive.ComputeHeadroom(ctx, s.store, g)
		if err != nil {
			return map[string]any{"status": "na", "summary": "계산 실패"}
		}
		cautionRatio := s.cfg.PredictiveSafety.NH3CautionRatio
		if cautionRatio == 0 {
			cautionRatio = 0.7
		}
		status := "ok"
		if hr.MaxProcessingKgPerH > 0 {
			switch {
			case hr.ActiveLoadKgPerH >= hr.MaxProcessingKgPerH:
				status = "breach"
			case hr.ActiveLoadKgPerH >= hr.MaxProcessingKgPerH*cautionRatio:
				status = "caution"
			}
		}
		return map[string]any{
			"status":            status,
			"headroom_kg_per_h": hr.HeadroomKgPerH,
			"capacity_kg_per_h": hr.MaxProcessingKgPerH,
			"summary":           predictiveSummary(status, hr.HeadroomKgPerH),
		}
	}
	return map[string]any{"status": "na", "summary": "WTG 미발견"}
}

func predictiveSummary(status string, headroom float64) string {
	switch status {
	case "breach":
		return "NH3 용량 초과"
	case "caution":
		return "NH3 여유 부족"
	default:
		if headroom > 0 {
			return "NH3 여유 정상"
		}
		return "용량 미설정"
	}
}

// safetyGateLearned returns the C-3l learned rule gate state.
// 단순히 enabled rule 개수만 노출 — Check() 자체는 cycle 시점에 실행되므로
// "현재 활성" 상태는 enabled count 로 충분.
func (s *Server) safetyGateLearned(ctx context.Context) map[string]any {
	rules, err := s.store.ListLearnedRules(ctx, true)
	if err != nil {
		return map[string]any{"status": "na", "rules_enabled": 0, "summary": "조회 실패"}
	}
	count := len(rules)
	status := "ok"
	if count > 0 {
		status = "active"
	}
	return map[string]any{
		"status":        status,
		"rules_enabled": count,
		"summary":       learnedSummary(count),
	}
}

func learnedSummary(count int) string {
	if count == 0 {
		return "학습 규칙 없음"
	}
	return ""
}

// safetyGateEnvironmental returns the C-3w environmental gate state.
// land site 는 "na" (해상 케이지에만 적용).
func (s *Server) safetyGateEnvironmental(ctx context.Context, siteType, siteID string) map[string]any {
	if siteType != "marine" {
		return map[string]any{"status": "na", "summary": "해상 케이지 전용"}
	}
	if siteID == "" {
		return map[string]any{"status": "na", "summary": "Site 미연결"}
	}

	items, err := s.store.ListRecentEnvironmentalSnapshots(ctx, siteID, 1)
	if err != nil || len(items) == 0 {
		return map[string]any{"status": "na", "summary": "환경 데이터 없음"}
	}
	snap := items[0]

	// 임계값 — internal/environmental_safety/gate.go 상수와 일치.
	const (
		windMax    = 12.0
		waveMax    = 2.0
		tideLowMin = 30
	)
	status := "ok"
	summary := "환경 정상"

	wind := float64Val(snap.WindSpeedMS)
	wave := float64Val(snap.WaveHeightM)

	switch {
	case wind > windMax:
		status = "breach"
		summary = formatBreach("풍속", wind, "m/s")
	case wave > waveMax:
		status = "breach"
		summary = formatBreach("파고", wave, "m")
	case snap.TideMinutesToLow != nil && *snap.TideMinutesToLow >= 0 && *snap.TideMinutesToLow < tideLowMin:
		status = "breach"
		summary = "간조 임박"
	case wind >= windMax*0.8 || wave >= waveMax*0.8:
		status = "caution"
		summary = "해상 조건 주의"
	default:
		summary = formatOk(wind, wave)
	}

	return map[string]any{
		"status":        status,
		"summary":       summary,
		"wind_speed_ms": snap.WindSpeedMS,
		"wave_height_m": snap.WaveHeightM,
	}
}

func float64Val(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func formatBreach(label string, value float64, unit string) string {
	return fmt.Sprintf("%s %.1f %s 초과", label, value, unit)
}

func formatOk(wind, wave float64) string {
	if wind == 0 && wave == 0 {
		return "환경 정상"
	}
	return fmt.Sprintf("풍속 %.1f m/s · 파고 %.1f m", wind, wave)
}
