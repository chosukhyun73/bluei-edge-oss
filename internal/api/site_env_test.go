package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 장기 환경 일별: 수조 센서는 들어온 시각이 아니라 관측 시각 날짜(KST)로, 바깥 관측은 현장별로 묶는다.
func TestSiteEnvDaily(t *testing.T) {
	s := newTestServerWithApp(t)
	s.cfg.Site.SiteID = "cheonyang_01"
	ctx := context.Background()
	kst := time.FixedZone("KST", 9*3600)
	day := time.Now().In(kst).AddDate(0, 0, -400) // 1년보다 전 (2년 화면)
	d0 := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, kst)
	for i, v := range []float64{17.0, 19.0} {
		val := v
		at := d0.Add(time.Duration(8+i*10) * time.Hour) // 08시·18시 KST → 같은 날
		id := "imp_t_wt_" + at.UTC().Format("20060102T1504")
		if _, err := s.app.AppendEventWithID(ctx, id, "api", "platform_import", "platform_import",
			events.EventSensorReadingRecorded, id, events.SensorReadingPayload{
				ReadingID: id, SensorID: "station_ftgp7", Metric: events.MetricWaterTemperature, Value: &val,
				Unit: "°C", Quality: events.QualityOK, ObservedAt: at.UTC().Format(time.RFC3339Nano),
				Location: events.Location{TankID: "p_t03"}}); err != nil {
			t.Fatal(err)
		}
	}
	dist := 45.9
	if err := s.store.UpsertSiteEnvObservations(ctx, []storage.SiteEnvObservation{
		{SiteID: "cheonyang_01", Metric: "wind_speed", ObservedAt: d0.Add(1 * time.Hour), Value: 4, Source: "kma_buoy", StationName: "통영", DistanceKM: &dist},
		{SiteID: "cheonyang_01", Metric: "wind_speed", ObservedAt: d0.Add(23 * time.Hour), Value: 14, Source: "kma_buoy", StationName: "통영", DistanceKM: &dist},
		{SiteID: "other", Metric: "wind_speed", ObservedAt: d0.Add(2 * time.Hour), Value: 99, Source: "kma_buoy"},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleSiteEnvDaily(rec, httptest.NewRequest(http.MethodGet, "/v1/site-env/daily?days=730", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Tank []storage.EnvDailyAgg `json:"tank"`
		Site []storage.EnvDailyAgg `json:"site"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	want := d0.Format("2006-01-02")
	if len(out.Tank) != 1 || out.Tank[0].Date != want || out.Tank[0].Min != 17 || out.Tank[0].Max != 19 || out.Tank[0].Avg != 18 {
		t.Fatalf("tank daily by observed date %s, got %+v", want, out.Tank)
	}
	if len(out.Site) != 1 || out.Site[0].Date != want || out.Site[0].Max != 14 || out.Site[0].Count != 2 {
		t.Fatalf("site daily (KST 날짜, 다른 현장 제외), got %+v", out.Site)
	}

	rec = httptest.NewRecorder()
	s.handleSiteEnvDaily(rec, httptest.NewRequest(http.MethodGet, "/v1/site-env/daily?days=30", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Tank) != 0 || len(out.Site) != 0 {
		t.Fatalf("30일 기간 밖은 제외, got %+v %+v", out.Tank, out.Site)
	}
}
