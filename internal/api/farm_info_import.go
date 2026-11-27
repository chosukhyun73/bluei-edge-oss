package api

// 앱 어장 정보 → GX10 어장·현장 (앱 → GX10, docs/53 1단계).
//
// 기존 앱 어장에 붙으면 /gx10/farm-tanks 의 farm(면허번호·사업자명·주소·경계)으로 GX10 어장 1개와
// 현장 1개를 만든다. 없으면 대시보드가 "등록된 농장 없음"으로 보인다. 현장 ID 는 이 GX10 의
// site_id 를 써서 가져온 수조(SiteID=site_id)와 이어지게 한다. 앱이 정본이라 연결 동기화마다 덮어쓴다.

import (
	"context"
	"fmt"
	"log/slog"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

func (s *Server) importFarmInfo(ctx context.Context, f map[string]any, systemType string) {
	farmID := asString(f["id"]) // 플랫폼 farm uuid (소문자·숫자·- → GX10 ID 규칙에 맞음)
	siteID := s.cfg.Site.SiteID
	if !domainIDRe.MatchString(farmID) || !domainIDRe.MatchString(siteID) {
		slog.Warn("farm info import: farm/site id 형식 불일치 — 건너뜀", "farm_id", farmID, "site_id", siteID)
		return
	}
	name := asString(f["name"])
	operator := asString(f["operator_name"])
	if operator == "" {
		operator = name
	}
	meta := map[string]any{"source": "platform_import", "platform_farm_id": farmID,
		"name": name, "address": asString(f["address"])}
	if err := s.store.UpsertFarm(ctx, &farm.Farm{
		FarmID: farmID, LicenseNo: asString(f["license_no"]), Operator: operator,
		Sites: []string{siteID}, Metadata: meta,
	}); err != nil {
		slog.Error("farm info import: farm upsert failed", "err", err)
		return
	}
	tz := s.cfg.Site.Timezone
	if tz == "" {
		tz = "Asia/Seoul"
	}
	siteMeta := map[string]any{"source": "platform_import", "address": asString(f["address"])}
	var err error
	if systemType == "land_based" {
		loc := site.LandLocation{Address: asString(f["address"])}
		if lat, lng := asFloat(f["lat"]), asFloat(f["lng"]); lat != 0 || lng != 0 {
			loc.Coordinates = &site.Coordinates{Lat: lat, Lon: lng}
		}
		err = s.store.UpsertSiteLand(ctx, &site.SiteLand{SiteID: siteID, FarmID: farmID, Name: name,
			Location: loc, Timezone: tz, Metadata: siteMeta})
	} else {
		var pts []site.MarineGPSPoint
		boundary, _ := f["boundary"].([]any)
		for _, b := range boundary {
			if p, ok := b.(map[string]any); ok { // position 은 현장 안에서 유일해야 한다 → 꼭짓점 순번
				pts = append(pts, site.MarineGPSPoint{Position: fmt.Sprintf("p%d", len(pts)+1),
					Lat: asFloat(p["lat"]), Lon: asFloat(p["lng"])})
			}
		}
		err = s.store.UpsertSiteMarine(ctx, &site.SiteMarine{SiteID: siteID, FarmID: farmID, Name: name,
			Location: site.MarineLocation{GPSPoints: pts}, Timezone: tz, Metadata: siteMeta})
	}
	if err != nil {
		slog.Error("farm info import: site upsert failed", "err", err)
		return
	}
	slog.Info("farm info import ok", "farm", name, "site_id", siteID)
}

// importFarmGroup — 어장 이름으로 그룹 1개(ID = 이 GX10 site_id). 대시보드 현장·수조 탭은 그룹을
// 골라야 수조가 보이므로, 그룹이 없으면 가져온 가두리가 화면에 안 나온다. 실패하면 "" (그룹 없이 계속).
func (s *Server) importFarmGroup(ctx context.Context, f map[string]any) string {
	groupID := s.cfg.Site.SiteID
	if !domainIDRe.MatchString(groupID) {
		return ""
	}
	if err := s.store.UpsertGroupProfile(ctx, &config.GroupProfile{
		GroupID: groupID, Name: asString(f["name"]), Color: "#0ea5e9",
		Metadata: map[string]any{"source": "platform_import", "platform_farm_id": asString(f["id"])},
	}); err != nil {
		slog.Error("farm group import failed", "err", err)
		return ""
	}
	return groupID
}

// applySpeciesBasis — 서버가 내려준 입식 어종 기준(앱 분석과 같은 값)을 수조에 붙인다.
//   - target_ranges 의 water_temperature = 적정 수온(lo~hi). 대시보드 그래프 띠·센서 표 표시용
//     (경보 규칙은 이 값을 안 쓴다). 없으면 대시보드가 연어 RAS 기준으로 그리던 문제 해결.
//   - metadata: species_code·species_model·growth_profiles·risk_rules (장기 환경 분석 화면용).
func applySpeciesBasis(p *storage.TankProfile, m map[string]any) {
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	for _, k := range []string{"species_code", "species_model", "growth_profiles", "risk_rules"} {
		if v, ok := m[k]; ok && v != nil {
			p.Metadata[k] = v
		}
	}
	model, _ := m["species_model"].(map[string]any)
	lo, okLo := model["temp_opt_lo"].(float64)
	hi, okHi := model["temp_opt_hi"].(float64)
	if !okLo || !okHi || lo >= hi {
		return
	}
	ranges := make([]storage.MetricRange, 0, len(p.TargetRanges)+1)
	for _, r := range p.TargetRanges {
		if r.Metric != events.MetricWaterTemperature {
			ranges = append(ranges, r)
		}
	}
	p.TargetRanges = append(ranges, storage.MetricRange{Metric: events.MetricWaterTemperature, Min: &lo, Max: &hi, Unit: "°C"})
}
