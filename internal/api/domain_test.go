package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

// domainMigrations are the migration files needed for domain tables (009-013).
var domainMigrations = []string{
	"009_farms_sites.sql",
	"010_water_treatment_groups.sql",
	"011_controllers.sql",
	"012_actuators_sensors.sql",
	"013_species_profiles.sql",
}

// newDomainTestStore returns a store with all migrations 001-013 applied.
func newDomainTestStore(t *testing.T) storage.Store {
	t.Helper()
	st := newTestStore(t) // applies 001-008
	migDir := findMigDir(t)
	for _, name := range domainMigrations {
		if err := storage.Migrate(st, filepath.Join(migDir, name)); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

func newDomainTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	return &Server{cfg: cfg, store: newDomainTestStore(t)}
}

// --- Controller registration tests ---

func TestControllerRegisterAndList(t *testing.T) {
	s := newDomainTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"mac_address":      "AA:BB:CC:DD:EE:01",
		"controller_id":    "ctrl_01",
		"firmware_version": "v0.2.0",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleControllerRoute(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var regResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &regResp)
	if regResp["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", regResp["status"])
	}
	if regResp["controller_id"] != "ctrl_01" {
		t.Errorf("expected controller_id=ctrl_01, got %v", regResp["controller_id"])
	}

	// GET /v1/controllers — must list the new entry with status=pending
	req2 := httptest.NewRequest(http.MethodGet, "/v1/controllers", nil)
	w2 := httptest.NewRecorder()
	s.handleControllerRoute(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var listResp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &listResp)
	items, _ := listResp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["status"] != "pending" {
		t.Errorf("list item status: got %v", item["status"])
	}
}

func TestControllerRegisterDuplicateMACConflict(t *testing.T) {
	s := newDomainTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"mac_address":   "AA:BB:CC:DD:EE:02",
		"controller_id": "ctrl_A",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleControllerRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first register: %d %s", w.Code, w.Body.String())
	}

	// Same MAC, different controller_id → 409
	body2, _ := json.Marshal(map[string]any{
		"mac_address":   "AA:BB:CC:DD:EE:02",
		"controller_id": "ctrl_B",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	s.handleControllerRoute(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate MAC: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestControllerRegisterIdempotent(t *testing.T) {
	s := newDomainTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"mac_address":   "AA:BB:CC:DD:EE:03",
		"controller_id": "ctrl_C",
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleControllerRoute(w, req)
		// First call → 201, second call (same MAC+ID) → 200
		if i == 0 && w.Code != http.StatusCreated {
			t.Fatalf("first: expected 201, got %d", w.Code)
		}
		if i == 1 && w.Code != http.StatusOK {
			t.Fatalf("idempotent: expected 200, got %d", w.Code)
		}
	}
}

// --- Controller activate tests ---

func TestControllerActivateHappyPath(t *testing.T) {
	s := newDomainTestServer(t)

	// Register first
	body, _ := json.Marshal(map[string]any{
		"mac_address":   "AA:BB:CC:DD:EE:04",
		"controller_id": "ctrl_D",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleControllerRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}

	// Activate
	req2 := httptest.NewRequest(http.MethodPost, "/v1/controllers/ctrl_D/activate", bytes.NewReader([]byte("{}")))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	s.handleControllerRoute(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("activate: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["status"] != "active" {
		t.Errorf("expected status=active, got %v", resp["status"])
	}
}

func TestControllerActivateAlreadyActive(t *testing.T) {
	s := newDomainTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"mac_address":   "AA:BB:CC:DD:EE:05",
		"controller_id": "ctrl_E",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleControllerRoute(w, req)

	// First activate — ok
	req2 := httptest.NewRequest(http.MethodPost, "/v1/controllers/ctrl_E/activate", bytes.NewReader([]byte("{}")))
	w2 := httptest.NewRecorder()
	s.handleControllerRoute(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("first activate: %d %s", w2.Code, w2.Body.String())
	}

	// Second activate — 409
	req3 := httptest.NewRequest(http.MethodPost, "/v1/controllers/ctrl_E/activate", bytes.NewReader([]byte("{}")))
	w3 := httptest.NewRecorder()
	s.handleControllerRoute(w3, req3)
	if w3.Code != http.StatusConflict {
		t.Fatalf("double activate: expected 409, got %d: %s", w3.Code, w3.Body.String())
	}
}

// --- GET /v1/farms ---

func TestGetFarmsEmpty(t *testing.T) {
	s := newDomainTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/farms", nil)
	w := httptest.NewRecorder()
	s.handleGetFarms(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}
}

func TestGetFarmsAfterUpsert(t *testing.T) {
	s := newDomainTestServer(t)
	ctx := context.Background()

	if err := s.store.UpsertFarm(ctx, &farm.Farm{
		FarmID:    "farm_01",
		LicenseNo: "LIC-001",
		Operator:  "TestOp",
	}); err != nil {
		t.Fatalf("UpsertFarm: %v", err)
	}
	if err := s.store.UpsertFarm(ctx, &farm.Farm{
		FarmID:    "farm_02",
		LicenseNo: "LIC-002",
		Operator:  "TestOp2",
	}); err != nil {
		t.Fatalf("UpsertFarm: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/farms", nil)
	w := httptest.NewRecorder()
	s.handleGetFarms(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["farm_id"] != "farm_01" {
		t.Errorf("expected farm_01 first, got %v", first["farm_id"])
	}
}

// --- POST round-trip tests (C-6a 신규 등록 라우트) ---

func TestPostFarmRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	body := []byte(`{"farm_id":"farm_post_01","license_no":"LIC-X","operator":"OpX"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/farms", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleFarmsRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/farms: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	// GET 으로 round-trip 확인
	w2 := httptest.NewRecorder()
	s.handleFarmsRoute(w2, httptest.NewRequest(http.MethodGet, "/v1/farms", nil))
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 farm after POST, got %d", len(items))
	}
}

func TestPostFarmValidation(t *testing.T) {
	s := newDomainTestServer(t)
	// 잘못된 farm_id (대문자) → 422
	body := []byte(`{"farm_id":"FARM_X","operator":"OpX"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/farms", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleFarmsRoute(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostSiteLandRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	// FK: 부모 Farm 먼저 등록.
	if err := s.store.UpsertFarm(context.Background(), &farm.Farm{FarmID: "f1", Operator: "Op"}); err != nil {
		t.Fatalf("seed farm: %v", err)
	}
	body := []byte(`{"site_id":"site_l","farm_id":"f1","site_type":"land","name":"강릉 RAS","timezone":"Asia/Seoul","address":"강릉"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/sites", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSitesRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/sites land: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	w2 := httptest.NewRecorder()
	s.handleSitesRoute(w2, httptest.NewRequest(http.MethodGet, "/v1/sites", nil))
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 site after POST, got %d", len(items))
	}
}

func TestPostSiteMarineRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	if err := s.store.UpsertFarm(context.Background(), &farm.Farm{FarmID: "f1", Operator: "Op"}); err != nil {
		t.Fatalf("seed farm: %v", err)
	}
	heading := 45.0
	body, _ := json.Marshal(map[string]any{
		"site_id":     "site_m",
		"farm_id":     "f1",
		"site_type":   "marine",
		"name":        "통영 가두리",
		"heading_deg": heading,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/sites", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSitesRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/sites marine: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostWTGRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	ctx := context.Background()
	if err := s.store.UpsertFarm(ctx, &farm.Farm{FarmID: "f1", Operator: "Op"}); err != nil {
		t.Fatalf("seed farm: %v", err)
	}
	if err := s.store.UpsertSiteLand(ctx, &site.SiteLand{SiteID: "site_l", FarmID: "f1", Name: "S", Timezone: "Asia/Seoul"}); err != nil {
		t.Fatalf("seed site: %v", err)
	}
	body := []byte(`{"wtg_id":"wtg_01","site_id":"site_l","name":"WTG A","tank_ids":["t1","t2"]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/water-treatment-groups", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleWTGsRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/water-treatment-groups: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	w2 := httptest.NewRecorder()
	s.handleWTGsRoute(w2, httptest.NewRequest(http.MethodGet, "/v1/water-treatment-groups", nil))
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 wtg after POST, got %d", len(items))
	}
}

func TestPostTankRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	body := []byte(`{"tank_id":"ras_tank_99","display_name":"테스트 수조","species":"atlantic_salmon","system_type":"land_based_ras","volume_m3":12.5}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tanks", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleTanksRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/tanks: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	w2 := httptest.NewRecorder()
	s.handleTanksRoute(w2, httptest.NewRequest(http.MethodGet, "/v1/tanks", nil))
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 tank after POST, got %d", len(items))
	}
}

func TestPostSensorRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	body := []byte(`{"sensor_id":"sensor_t_99","sensor_type":"temperature","tank_id":"ras_tank_99","capabilities":["temperature"]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/sensors", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSensorsRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/sensors: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostActuatorRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	body := []byte(`{"device_id":"feeder_99","device_type":"feeder","tank_id":"ras_tank_99","capabilities":["dispense"]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/actuators", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleActuatorsRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/actuators: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostSpeciesProfileRoundTrip(t *testing.T) {
	s := newDomainTestServer(t)
	body := []byte(`{"species":"test_species","display_name":"테스트종"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/species-profiles", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleSpeciesProfilesRoute(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /v1/species-profiles: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// --- GET /v1/controllers?status= filter ---

func TestGetControllersStatusFilter(t *testing.T) {
	s := newDomainTestServer(t)

	// Register two controllers
	for _, id := range []string{"ctrl_F", "ctrl_G"} {
		mac := "AA:BB:CC:DD:EE:0" + id[len(id)-1:]
		body, _ := json.Marshal(map[string]any{
			"mac_address":   mac,
			"controller_id": id,
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/controllers/register", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleControllerRoute(w, req)
	}

	// Activate ctrl_F only
	req := httptest.NewRequest(http.MethodPost, "/v1/controllers/ctrl_F/activate", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	s.handleControllerRoute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("activate ctrl_F: %d %s", w.Code, w.Body.String())
	}

	// ?status=pending → only ctrl_G
	req2 := httptest.NewRequest(http.MethodGet, "/v1/controllers?status=pending", nil)
	w2 := httptest.NewRecorder()
	s.handleControllerRoute(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list pending: %d %s", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 pending, got %d", len(items))
	}
}
