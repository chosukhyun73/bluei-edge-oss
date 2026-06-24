package waterquality

import (
	"strings"
	"testing"

	"bluei.kr/edge/internal/events"
)

func TestParseSmartSalmonCSVBuildsSensorReadings(t *testing.T) {
	csv := "동 이름,모듈 번호,수조 번호,수온,pH,DO,저장 시점\n" +
		"연구동,6,1,22.2,9.43,11.02,2026-04-28 18:07:25.351 +0900\n" +
		"연구동,6,1,0,13.52,0,2026-04-28 18:09:25.351 +0900\n"

	readings, err := ParseSmartSalmonCSV(strings.NewReader(csv), SmartSalmonOptions{
		SiteID:   "site_test",
		EdgeID:   "edge_test",
		DeviceID: "dws7000b_mcsc_01",
	})
	if err != nil {
		t.Fatalf("ParseSmartSalmonCSV error: %v", err)
	}
	if len(readings) != 6 {
		t.Fatalf("readings len = %d, want 6", len(readings))
	}

	first := readings[0]
	if first.Metric != events.MetricWaterTemperature {
		t.Fatalf("first metric = %q", first.Metric)
	}
	if first.Unit != "celsius" || first.Value == nil || *first.Value != 22.2 {
		t.Fatalf("first value/unit = %v %q", first.Value, first.Unit)
	}
	if first.ObservedAt != "2026-04-28T09:07:25.351Z" {
		t.Fatalf("observed_at = %q", first.ObservedAt)
	}
	if first.Location.TankID != "tank_1" {
		t.Fatalf("tank_id = %q", first.Location.TankID)
	}
	if first.Raw["module_no"] != "6" || first.Raw["building"] != "연구동" {
		t.Fatalf("raw metadata not preserved: %#v", first.Raw)
	}

	again, err := ParseSmartSalmonCSV(strings.NewReader(csv), SmartSalmonOptions{
		SiteID:   "site_test",
		EdgeID:   "edge_test",
		DeviceID: "dws7000b_mcsc_01",
	})
	if err != nil {
		t.Fatalf("second ParseSmartSalmonCSV error: %v", err)
	}
	if first.ReadingID == "" || first.ReadingID != again[0].ReadingID {
		t.Fatalf("reading_id should be deterministic, got %q and %q", first.ReadingID, again[0].ReadingID)
	}
	otherSite, err := ParseSmartSalmonCSV(strings.NewReader(csv), SmartSalmonOptions{
		SiteID:   "site_other",
		EdgeID:   "edge_test",
		DeviceID: "dws7000b_mcsc_01",
	})
	if err != nil {
		t.Fatalf("other-site ParseSmartSalmonCSV error: %v", err)
	}
	if first.ReadingID == otherSite[0].ReadingID {
		t.Fatalf("reading_id should include site scope: %q", first.ReadingID)
	}

	if readings[3].Quality != events.QualitySuspect || readings[3].Raw["quality_reason"] != ReasonInvalidZero {
		t.Fatalf("zero temperature quality = %q raw=%#v", readings[3].Quality, readings[3].Raw)
	}
	if readings[4].Quality != events.QualitySuspect || readings[4].Raw["quality_reason"] != ReasonInvalidRange {
		t.Fatalf("high pH quality = %q raw=%#v", readings[4].Quality, readings[4].Raw)
	}
	if readings[5].Quality != events.QualitySuspect || readings[5].Raw["quality_reason"] != ReasonInvalidZero {
		t.Fatalf("zero DO quality = %q raw=%#v", readings[5].Quality, readings[5].Raw)
	}
}
