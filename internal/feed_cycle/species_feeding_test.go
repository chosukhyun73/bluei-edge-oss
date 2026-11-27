package feed_cycle

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

const sampleFeedingJSON = `{"species":{
  "red_sea_bream":{"code":"REV","name":"참돔","dt80":{"model":"DT80_EXP","k":70.0,"b":0.76,"c":0.08},
    "feeding_rules":[
      {"temp_min":null,"temp_max":10.0,"feed_rate_pct":null,"note":"급이 중단"},
      {"temp_min":10.0,"temp_max":12.0,"feed_rate_pct":0.3,"pellet_mm":3.0,"note":"유지"},
      {"temp_min":22.0,"temp_max":24.0,"feed_rate_pct":1.9,"pellet_mm":5.0,"note":"최적"}
    ]},
  "no_dt80":{"code":"XXX","name":"노DT80","dt80":null,
    "feeding_rules":[{"temp_min":15.0,"temp_max":25.0,"feed_rate_pct":1.0}]}
}}`

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "species-feeding.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSpeciesFeeding_MissingAndEmpty(t *testing.T) {
	m, err := LoadSpeciesFeeding("")
	if err != nil || len(m) != 0 {
		t.Fatalf("empty path → empty map, got %v err %v", m, err)
	}
	m, err = LoadSpeciesFeeding(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(m) != 0 {
		t.Fatalf("missing file → empty map(no error), got %v err %v", m, err)
	}
}

func TestFeedRateAt(t *testing.T) {
	m, err := LoadSpeciesFeeding(writeTemp(t, sampleFeedingJSON))
	if err != nil {
		t.Fatal(err)
	}
	rev := m["red_sea_bream"]
	// 9°C → 급이중단 구간(feed_rate null) → ok=false (절식)
	if _, ok := rev.FeedRateAt(9.0); ok {
		t.Error("9°C should be fasting (ok=false)")
	}
	// 11°C → 0.3%
	if p, ok := rev.FeedRateAt(11.0); !ok || p != 0.3 {
		t.Errorf("11°C → 0.3%%, got %v ok=%v", p, ok)
	}
	// 23°C → 1.9%
	if p, ok := rev.FeedRateAt(23.0); !ok || p != 1.9 {
		t.Errorf("23°C → 1.9%%, got %v ok=%v", p, ok)
	}
	// 30°C → 매칭 구간 없음 → 절식
	if _, ok := rev.FeedRateAt(30.0); ok {
		t.Error("30°C (범위 밖) should be fasting")
	}
}

func TestDT80Hours(t *testing.T) {
	m, _ := LoadSpeciesFeeding(writeTemp(t, sampleFeedingJSON))
	// 참돔 1kg, 14°C: 70·1^(1-0.76)·exp(-0.08·14) = 70·exp(-1.12) ≈ 22.84
	h, ok := m["red_sea_bream"].DT80Hours(1.0, 14.0)
	if !ok || math.Abs(h-22.84) > 0.1 {
		t.Errorf("참돔 DT80(1kg,14°C) ≈ 22.84, got %v ok=%v", h, ok)
	}
	// DT80 미보유 → ok=false (2b 에서 보수적 폴백)
	if _, ok := m["no_dt80"].DT80Hours(1.0, 14.0); ok {
		t.Error("no_dt80 should return ok=false")
	}
}
