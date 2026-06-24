package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

// seedReading inserts a raw sensor.reading.recorded with a controlled recorded_at.
func seedReading(t *testing.T, store storage.Store, id, sensorID, metric string, value float64, at time.Time) {
	t.Helper()
	payload := map[string]any{"sensor_id": sensorID, "metric": metric, "value": value, "unit": "mg/L"}
	pj, _ := json.Marshal(payload)
	if _, err := store.AppendEvent(context.Background(), &storage.Event{
		EventID:       id,
		EventType:     "sensor.reading.recorded",
		SchemaVersion: "1.0",
		SiteID:        "site_test",
		EdgeID:        "edge_test",
		RecordedAt:    at,
		SourceModule:  "collector",
		PayloadJSON:   string(pj),
		EventJSON:     `{}`,
	}); err != nil {
		t.Fatalf("seed reading %s: %v", id, err)
	}
}

func TestSummarizeSensorReadingsDaily(t *testing.T) {
	ctx := context.Background()
	app, store := openProjectionTestApp(t)
	loc := time.UTC

	now := time.Now().UTC()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	twoDaysAgo := startToday.AddDate(0, 0, -2).Add(10 * time.Hour) // 이틀 전 10:00
	dayLabel := twoDaysAgo.Format("2006-01-02")

	// 이틀 전: DO 6.0, 8.0 (min6 max8 avg7 n2) — 요약 대상.
	seedReading(t, store, "r1", "sensor_do_tank_01", "dissolved_oxygen", 6.0, twoDaysAgo)
	seedReading(t, store, "r2", "sensor_do_tank_01", "dissolved_oxygen", 8.0, twoDaysAgo.Add(time.Hour))
	// 이틀 전: 다른 메트릭 pH 7.0 (별도 그룹).
	seedReading(t, store, "r3", "sensor_ph_tank_01", "ph", 7.0, twoDaysAgo)
	// 오늘: 요약 대상 아님 (recorded_at >= before).
	seedReading(t, store, "r4", "sensor_do_tank_01", "dissolved_oxygen", 99.0, startToday.Add(time.Hour))

	emitted, err := app.SummarizeSensorReadingsDaily(ctx, startToday, loc)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if emitted != 2 {
		t.Fatalf("expected 2 summaries (do, ph for one day), got %d", emitted)
	}

	// 요약 이벤트 검증.
	evs, err := store.QueryEvents(ctx, storage.EventFilter{EventType: SensorDailySummaryEventType, Limit: 100})
	if err != nil {
		t.Fatalf("query summaries: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("expected 2 summary events, got %d", len(evs))
	}
	var doFound bool
	for _, e := range evs {
		var p map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p["metric"] == "dissolved_oxygen" {
			doFound = true
			if p["date"] != dayLabel {
				t.Errorf("DO summary date = %v, want %s", p["date"], dayLabel)
			}
			if p["min"].(float64) != 6.0 || p["max"].(float64) != 8.0 || p["avg"].(float64) != 7.0 {
				t.Errorf("DO min/max/avg = %v/%v/%v, want 6/8/7", p["min"], p["max"], p["avg"])
			}
			if int(p["count"].(float64)) != 2 {
				t.Errorf("DO count = %v, want 2", p["count"])
			}
		}
	}
	if !doFound {
		t.Fatal("dissolved_oxygen summary not found")
	}

	// 멱등성: 재실행 시 0건 (이미 요약됨), 오늘 데이터는 여전히 제외.
	emitted2, err := app.SummarizeSensorReadingsDaily(ctx, startToday, loc)
	if err != nil {
		t.Fatalf("summarize re-run: %v", err)
	}
	if emitted2 != 0 {
		t.Fatalf("re-run should emit 0 (idempotent), got %d", emitted2)
	}
}

// deterministic event_id 형식 가드.
func TestDailySummaryEventID(t *testing.T) {
	got := dailySummaryEventID("2026-05-24", "sensor_do_tank_01", "dissolved_oxygen")
	want := fmt.Sprintf("dsum-%s-%s-%s", "2026-05-24", "sensor_do_tank_01", "dissolved_oxygen")
	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
}
