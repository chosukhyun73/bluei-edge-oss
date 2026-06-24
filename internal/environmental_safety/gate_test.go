package environmental_safety

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	st "bluei.kr/edge/internal/storage"
)

// --- minimal mock store (embed st.Store to satisfy interface) ---

type mockEnvStore struct {
	st.Store // embed; unused methods panic if called
	profile  *st.TankProfile
	sites    []map[string]any
}

func (m *mockEnvStore) GetTankProfile(_ context.Context, _ string) (*st.TankProfile, error) {
	return m.profile, nil
}

func (m *mockEnvStore) ListSites(_ context.Context, _ string) ([]map[string]any, error) {
	return m.sites, nil
}

// --- helpers ---

func calmCfg() config.EnvironmentalSafetyConfig {
	return config.EnvironmentalSafetyConfig{
		Enabled:   true,
		Source:    "mock",
		WindMaxMS: 12.0,
		WaveMaxM:  2.0,
	}
}

func buildTestGate(windMS, waveM float64, tideMin int, profile *st.TankProfile, sites []map[string]any) *Gate {
	src := &MockSource{
		WindSpeedMS:      windMS,
		WaveHeightM:      waveM,
		TideMinutesToLow: tideMin,
		TidePhase:        "falling",
		TemperatureC:     15,
	}
	store := &mockEnvStore{profile: profile, sites: sites}
	g := &Gate{
		cfg:   calmCfg(),
		store: store,
		cache: newCache(src, time.Second),
		log:   silentLog(),
	}
	return g
}

func marineProfile(tankID, siteID string) *st.TankProfile {
	p := &st.TankProfile{}
	p.TankID = tankID
	p.SiteID = siteID
	return p
}

func marineSiteRow(siteID string) map[string]any {
	return map[string]any{"site_id": siteID, "site_type": "marine"}
}

func landProfile(tankID string) *st.TankProfile {
	p := &st.TankProfile{}
	p.TankID = tankID
	return p // SiteID empty → land
}

// --- tests ---

// TestGate_CalmConditions_Marine_Allow — 잔잔한 날씨 해상 cage → 허용.
func TestGate_CalmConditions_Marine_Allow(t *testing.T) {
	g := buildTestGate(5.0, 0.8, 120, marineProfile("cage-01", "site-marine-01"), []map[string]any{marineSiteRow("site-marine-01")})

	blocked, reason := g.Check("cage-01")
	if blocked {
		t.Fatalf("expected allow in calm conditions, got block: %s", reason)
	}
}

// TestGate_HighWind_Marine_Block — 강풍(>12 m/s) 해상 cage → 차단.
func TestGate_HighWind_Marine_Block(t *testing.T) {
	g := buildTestGate(15.0, 0.5, 120, marineProfile("cage-01", "site-marine-01"), []map[string]any{marineSiteRow("site-marine-01")})

	blocked, reason := g.Check("cage-01")
	if !blocked {
		t.Fatal("expected block due to high wind")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

// TestGate_LandTank_AlwaysAllow — 육상 RAS → 환경 조건 무관 항상 허용.
func TestGate_LandTank_AlwaysAllow(t *testing.T) {
	// extreme wind, but land tank → must allow
	g := buildTestGate(50.0, 10.0, 0, landProfile("tank-ras-01"), nil)

	blocked, _ := g.Check("tank-ras-01")
	if blocked {
		t.Fatal("land RAS tank must always allow regardless of environmental conditions")
	}
}

// TestGate_HighWave_Marine_Block — 파고 초과 → 차단.
func TestGate_HighWave_Marine_Block(t *testing.T) {
	g := buildTestGate(5.0, 3.5, 120, marineProfile("cage-02", "site-marine-02"), []map[string]any{marineSiteRow("site-marine-02")})

	blocked, reason := g.Check("cage-02")
	if !blocked {
		t.Fatal("expected block due to high wave")
	}
	_ = reason
}

// TestGate_TideLow_Marine_Block — 간조 임박(< 30분) → 차단.
func TestGate_TideLow_Marine_Block(t *testing.T) {
	g := buildTestGate(5.0, 0.5, 10, marineProfile("cage-03", "site-marine-03"), []map[string]any{marineSiteRow("site-marine-03")})

	blocked, reason := g.Check("cage-03")
	if !blocked {
		t.Fatalf("expected block when tide_low_in=10 < 30 min, reason=%s", reason)
	}
}

// TestGate_Disabled_AlwaysAllow — 비활성 게이트 → 항상 허용.
func TestGate_Disabled_AlwaysAllow(t *testing.T) {
	src := &MockSource{WindSpeedMS: 50, WaveHeightM: 10}
	g := &Gate{
		cfg:   config.EnvironmentalSafetyConfig{Enabled: false},
		store: &mockEnvStore{profile: marineProfile("cage-04", "site-01"), sites: []map[string]any{marineSiteRow("site-01")}},
		cache: newCache(src, time.Second),
		log:   silentLog(),
	}

	blocked, _ := g.Check("cage-04")
	if blocked {
		t.Fatal("disabled gate must always allow")
	}
}
