package environmental_safety

import "time"

// Reading is a single environmental observation.
type Reading struct {
	SiteID           string
	WindSpeedMS      float64 // m/s
	WaveHeightM      float64 // m
	TideMinutesToLow int     // 다음 간조까지 남은 분 (0 = 현재 간조)
	TidePhase        string  // "rising" | "falling" | "high" | "low"
	TemperatureC     float64
	RecordedAt       time.Time
	Source           string
}

// Source abstracts the environmental data provider.
type Source interface {
	Fetch(siteID string) (*Reading, error)
}

// MockSource returns configurable static values — offline/test 용.
type MockSource struct {
	WindSpeedMS      float64
	WaveHeightM      float64
	TideMinutesToLow int
	TidePhase        string
	TemperatureC     float64
}

func (m *MockSource) Fetch(siteID string) (*Reading, error) {
	return &Reading{
		SiteID:           siteID,
		WindSpeedMS:      m.WindSpeedMS,
		WaveHeightM:      m.WaveHeightM,
		TideMinutesToLow: m.TideMinutesToLow,
		TidePhase:        m.TidePhase,
		TemperatureC:     m.TemperatureC,
		RecordedAt:       time.Now().UTC(),
		Source:           "mock",
	}, nil
}

// HTTPSource is a stub for the real weather/tide API.
// TODO(real-API): integrate with 기상청 openAPI key + 해양수산부 조수 API
type HTTPSource struct {
	Endpoint string
}

func (h *HTTPSource) Fetch(siteID string) (*Reading, error) {
	// TODO(real-API): HTTP GET to h.Endpoint, parse response
	return &Reading{
		SiteID:           siteID,
		WindSpeedMS:      0,
		WaveHeightM:      0,
		TideMinutesToLow: 9999,
		TidePhase:        "unknown",
		RecordedAt:       time.Now().UTC(),
		Source:           "http_stub",
	}, nil
}
