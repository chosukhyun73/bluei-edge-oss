package environmental_safety

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

const (
	defaultWindMaxMS      = 12.0 // m/s — 해상 케이지 기본 풍속 임계값
	defaultWaveMaxM       = 2.0  // m
	defaultTideLowMinutes = 30   // 간조 30분 전 차단
)

// Gate implements feed_cycle.SafetyGate for C-3w environmental conditions.
//
// 해상(marine) 케이지에만 적용. 육상 RAS 는 항상 허용.
// site_type 은 storage.ListSites 로 조회 (캐시 없음 — 변경 빈도 극히 낮음).
type Gate struct {
	cfg   config.EnvironmentalSafetyConfig
	store storage.Store
	cache *Cache
	log   *slog.Logger
}

// NewGate creates a C-3w environmental safety gate.
// source 가 nil 이면 MockSource(calm) 를 사용.
func NewGate(store storage.Store, cfg config.EnvironmentalSafetyConfig, source Source) *Gate {
	if source == nil {
		source = &MockSource{
			WindSpeedMS:      0,
			WaveHeightM:      0,
			TideMinutesToLow: 9999,
			TidePhase:        "high",
			TemperatureC:     15,
		}
	}
	ttl := time.Duration(cfg.RefreshIntervalSec) * time.Second
	return &Gate{
		cfg:   cfg,
		store: store,
		cache: newCache(source, ttl),
		log:   slog.With("component", "environmental_gate"),
	}
}

// StartRefresh begins background cache refresh for the given siteIDs.
func (g *Gate) StartRefresh(ctx context.Context, siteIDs []string) {
	interval := time.Duration(g.cfg.RefreshIntervalSec) * time.Second
	if interval <= 0 {
		interval = defaultCacheTTL
	}
	g.cache.StartRefresh(ctx, siteIDs, interval)
}

// Check implements feed_cycle.SafetyGate.
// 해상 site 의 tank_id 에만 환경 조건을 적용.
func (g *Gate) Check(tankID string) (bool, string) {
	if !g.cfg.Enabled {
		return false, ""
	}

	// tank 의 site_type 조회 — storage 에서 직접 (불가능 시 fail-open)
	siteType, siteID, err := g.tankSiteType(tankID)
	if err != nil {
		g.log.Warn("C-3w site lookup failed; failing open", "tank_id", tankID, "error", err)
		return false, ""
	}

	// 육상 RAS 는 환경 조건 무관 — 항상 허용
	if siteType != "marine" {
		return false, ""
	}

	reading := g.cache.Get(siteID)
	if reading == nil {
		g.log.Warn("C-3w no environmental reading; failing open", "tank_id", tankID, "site_id", siteID)
		return false, ""
	}

	windMax := g.cfg.WindMaxMS
	if windMax == 0 {
		windMax = defaultWindMaxMS
	}
	waveMax := g.cfg.WaveMaxM
	if waveMax == 0 {
		waveMax = defaultWaveMaxM
	}

	// 풍속 초과
	if reading.WindSpeedMS > windMax {
		reason := fmt.Sprintf("wind_speed=%.1f m/s > max=%.1f m/s", reading.WindSpeedMS, windMax)
		g.log.Warn("C-3w BLOCK wind", "tank_id", tankID, "site_id", siteID, "reason", reason)
		return true, reason
	}

	// 파고 초과
	if reading.WaveHeightM > waveMax {
		reason := fmt.Sprintf("wave_height=%.2f m > max=%.2f m", reading.WaveHeightM, waveMax)
		g.log.Warn("C-3w BLOCK wave", "tank_id", tankID, "site_id", siteID, "reason", reason)
		return true, reason
	}

	// 간조 임박 (조수 기반 급이 시기 조정)
	if reading.TideMinutesToLow >= 0 && reading.TideMinutesToLow < defaultTideLowMinutes {
		reason := fmt.Sprintf("tide_low_in=%d min < threshold=%d min", reading.TideMinutesToLow, defaultTideLowMinutes)
		g.log.Warn("C-3w BLOCK tide", "tank_id", tankID, "site_id", siteID, "reason", reason)
		return true, reason
	}

	return false, ""
}

// tankSiteType resolves site_type and site_id for a given tank_id.
// tank_profiles 에 site_id 필드가 있으면 그것으로 sites 테이블 조회.
// site_id 없으면 fail-open (site_type="land" 로 취급).
func (g *Gate) tankSiteType(tankID string) (siteType, siteID string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	profile, err := g.store.GetTankProfile(ctx, tankID)
	if err != nil {
		return "", "", err
	}
	if profile == nil || profile.SiteID == "" {
		// site_id 없으면 land RAS 로 취급 → 허용
		return "land", "", nil
	}

	sites, err := g.store.ListSites(ctx, "")
	if err != nil {
		return "", "", err
	}
	for _, s := range sites {
		if s["site_id"] == profile.SiteID {
			st, _ := s["site_type"].(string)
			return st, profile.SiteID, nil
		}
	}
	// site_id 있지만 sites 테이블에 없으면 land 로 취급
	return "land", profile.SiteID, nil
}
