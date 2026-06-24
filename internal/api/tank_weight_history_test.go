package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

// newTestStoreWithWeightHistory — D-5 migration 포함 test store.
func newTestStoreWithWeightHistory(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_wh.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	migDir := findMigDir(t)
	for _, name := range []string{
		"001_init.sql", "002_autonomous_mode.sql", "003_lifecycle.sql",
		"004_sampling.sql", "005_fcr_calibration.sql", "006_decision_policy.sql",
		"007_weight_history.sql",
		"030_traceability_lifecycle.sql",
	} {
		if err := storage.Migrate(st, filepath.Join(migDir, name)); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

func newTestServerWH(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	return &Server{cfg: cfg, store: newTestStoreWithWeightHistory(t)}
}

func getWeightHistory(t *testing.T, s *Server, tankID, query string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/v1/tanks/" + tankID + "/weight-history"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	s.handleTankWeightHistory(w, req)
	return w
}

func seedWeightSnap(t *testing.T, st storage.Store, tankID, date string, weight float64, quality string) {
	t.Helper()
	snap := &storage.TankWeightSnapshot{
		TankID:              tankID,
		SnapshotDate:        date,
		EstimatedAvgWeightG: weight,
		AnchorWeightG:       50.0,
		AnchorSource:        "stocking",
		DaysSinceAnchor:     10,
		ExpectedFCR:         1.4,
		FCRSource:           "default",
		CumulativeFeedG:     5000.0,
		Quality:             quality,
		SnapshotAt:          time.Now().UTC(),
	}
	if err := st.UpsertTankWeightSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("seedWeightSnap %s %s: %v", tankID, date, err)
	}
}

func TestGetWeightHistoryEmpty(t *testing.T) {
	s := newTestServerWH(t)
	w := getWeightHistory(t, s, "tank_empty_wh", "days=30")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp["count"].(float64) != 0 {
		t.Errorf("expected count=0, got %v", resp["count"])
	}
	snaps, ok := resp["snapshots"].([]any)
	if !ok || len(snaps) != 0 {
		t.Errorf("expected empty snapshots array, got %v", resp["snapshots"])
	}
}

func TestGetWeightHistoryWithData(t *testing.T) {
	s := newTestServerWH(t)

	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		date := base.AddDate(0, 0, -i).Format("2006-01-02")
		seedWeightSnap(t, s.store, "tank_data_wh", date, float64(50+i), "ok")
	}

	w := getWeightHistory(t, s, "tank_data_wh", "days=30")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp["count"].(float64) != 5 {
		t.Errorf("expected count=5, got %v", resp["count"])
	}
	snaps := resp["snapshots"].([]any)
	if len(snaps) != 5 {
		t.Fatalf("expected 5 snapshots, got %d", len(snaps))
	}
	// ASC 정렬 확인
	first := snaps[0].(map[string]any)["snapshot_date"].(string)
	last := snaps[4].(map[string]any)["snapshot_date"].(string)
	if first >= last {
		t.Errorf("expected ASC order: first=%q last=%q", first, last)
	}
}

func TestGetWeightHistoryInvalidDays(t *testing.T) {
	s := newTestServerWH(t)

	for _, q := range []string{"days=0", "days=400", "days=-1", "days=abc"} {
		w := getWeightHistory(t, s, "tank_x", q)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("query %q: expected 422, got %d: %s", q, w.Code, w.Body.String())
		}
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		errObj, _ := resp["error"].(map[string]any)
		if errObj == nil || errObj["code"] != "INVALID_DAYS_RANGE" {
			t.Errorf("query %q: expected error.code=INVALID_DAYS_RANGE, got %v", q, resp)
		}
	}
}

func TestGetWeightHistoryFiltersByDays(t *testing.T) {
	s := newTestServerWH(t)

	base := time.Now().UTC()
	// seed 60 snapshots over 60 days
	for i := 0; i < 60; i++ {
		date := base.AddDate(0, 0, -i).Format("2006-01-02")
		seedWeightSnap(t, s.store, "tank_filter_wh", date, float64(50+i), "ok")
	}

	// days=30 should return only last 30 days
	w := getWeightHistory(t, s, "tank_filter_wh", "days=30")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	count := int(resp["count"].(float64))
	// 30 or 31 days (inclusive date boundary may vary by 1)
	if count < 29 || count > 32 {
		t.Errorf("expected ~30 snapshots for days=30, got %d", count)
	}
	// should be < 60
	if count >= 60 {
		t.Errorf("days=30 filter not applied: got %d snapshots", count)
	}
}
