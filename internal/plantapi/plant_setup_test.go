package plantapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/site"
)

func TestPlantSetupRegistration(t *testing.T) {
	s := newTestServerWithApp(t)
	newProcessingSite(t, s)
	base := "/v1/plant/plant_01"

	// 공장 정보: 비어 있으면 추적 기록에 필요한 값이 빠졌다고 알려 준다
	w := httptest.NewRecorder()
	s.handlePlantRoute(w, httptest.NewRequest(http.MethodGet, base+"/profile", nil))
	if got := decodeMap(t, w)["profile"].(map[string]any)["missing_for_traceability"].([]any); len(got) != 2 {
		t.Fatalf("빈 공장 정보는 허가번호·인권 정책 2건 누락이어야 함: %v", got)
	}
	if w = plantDo(t, s, http.MethodPut, base+"/profile", map[string]any{"human_welfare_policy": "maybe"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("잘못된 인권 정책 값은 422: %d", w.Code)
	}
	w = plantDo(t, s, http.MethodPut, base+"/profile", map[string]any{
		"business_no": "123-45-67890", "license_name": "수산물가공업 신고", "license_no": "제2026-0001호",
		"license_authority": "부산광역시 사하구", "human_welfare_policy": "internal_policy",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("put profile: %d %s", w.Code, w.Body.String())
	}
	prof := decodeMap(t, w)["profile"].(map[string]any)
	if len(prof["missing_for_traceability"].([]any)) != 0 || prof["retention_years"].(float64) != 3 {
		t.Fatalf("채운 뒤에는 누락 0건, 보관기간 기본 3년: %v", prof)
	}

	// 구역 → 설비 → 센서. 없는 구역·설비를 가리키면 거부
	if w = plantDo(t, s, http.MethodPost, base+"/zones", map[string]any{"zone_id": "cold_1", "name": "원료 냉장고", "temp_class": "chilled"}); w.Code != http.StatusOK {
		t.Fatalf("zone: %d %s", w.Code, w.Body.String())
	}
	if w = plantDo(t, s, http.MethodPost, base+"/zones", map[string]any{"name": "x", "temp_class": "hot"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("잘못된 temp_class 는 422: %d", w.Code)
	}
	if w = plantDo(t, s, http.MethodPost, base+"/equipment", map[string]any{"equipment_id": "md_1", "zone_id": "nowhere", "kind": "metal_detector", "name": "금속검출기"}); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "UNKNOWN_ZONE") {
		t.Fatalf("없는 구역의 설비는 거부: %d %s", w.Code, w.Body.String())
	}
	if w = plantDo(t, s, http.MethodPost, base+"/equipment", map[string]any{"equipment_id": "md_1", "kind": "metal_detector", "name": "금속검출기"}); w.Code != http.StatusOK {
		t.Fatalf("equipment: %d %s", w.Code, w.Body.String())
	}
	if w = plantDo(t, s, http.MethodPost, base+"/sensors", map[string]any{"metric": "temperature"}); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "SENSOR_UNBOUND") {
		t.Fatalf("구역·설비 없이 센서 등록은 거부: %d %s", w.Code, w.Body.String())
	}
	if w = plantDo(t, s, http.MethodPost, base+"/sensors", map[string]any{"metric": "count", "equipment_id": "md_x"}); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "UNKNOWN_EQUIPMENT") {
		t.Fatalf("없는 설비의 센서는 거부: %d %s", w.Code, w.Body.String())
	}
	if w = plantDo(t, s, http.MethodPost, base+"/sensors", map[string]any{"sensor_id": "cold_1_temp", "zone_id": "cold_1", "metric": "temperature", "unit": "℃", "is_mock": true}); w.Code != http.StatusOK {
		t.Fatalf("sensor: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handlePlantRoute(w, httptest.NewRequest(http.MethodGet, base+"/sensors", nil))
	if decodeMap(t, w)["count"].(float64) != 1 {
		t.Fatalf("센서 1건이어야 함: %s", w.Body.String())
	}
}

func TestLocalUsersAndPIN(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	newProcessingSite(t, s)
	base := "/v1/plant/plant_01/users"

	for name, body := range map[string]map[string]any{
		"짧은 PIN": {"name": "홍", "role": "admin", "pin": "12"},
		"문자 PIN": {"name": "홍", "role": "admin", "pin": "abcd"},
		"없는 역할":  {"name": "홍", "role": "boss", "pin": "1234"},
		"이름 없음":  {"role": "admin", "pin": "1234"},
	} {
		if w := plantDo(t, s, http.MethodPost, base, body); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: 422 여야 함: %d %s", name, w.Code, w.Body.String())
		}
	}
	w := plantDo(t, s, http.MethodPost, base, map[string]any{"name": "홍길동", "role": "admin", "pin": "4321"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	// 응답과 목록 어디에도 PIN·해시·솔트가 나오면 안 된다
	lw := httptest.NewRecorder()
	s.handlePlantRoute(lw, httptest.NewRequest(http.MethodGet, base, nil))
	for _, body := range []string{w.Body.String(), lw.Body.String()} {
		if strings.Contains(body, "4321") || strings.Contains(body, "pin") || strings.Contains(body, "salt") || strings.Contains(body, "hash") {
			t.Fatalf("응답에 PIN 관련 값이 있으면 안 됨: %s", body)
		}
	}
	userID := decodeMap(t, w)["user"].(map[string]any)["user_id"].(string)
	stored, _ := s.store.GetLocalUser(ctx, userID)
	if stored.PinHash == "" || stored.PinHash == "4321" || len(stored.PinSalt) != 32 {
		t.Fatalf("PIN 은 솔트를 섞은 해시로만 저장되어야 함: %+v", stored)
	}

	if _, err := s.verifySigner(ctx, "plant_01", userID, "4321", roleAdmin); err != nil {
		t.Fatalf("맞는 PIN 은 통과: %v", err)
	}
	if _, err := s.verifySigner(ctx, "plant_01", userID, "0000", roleAdmin); err != errSignerPIN {
		t.Fatalf("틀린 PIN: %v", err)
	}
	if _, err := s.verifySigner(ctx, "plant_01", userID, "4321", roleReviewer); err != errSignerRole {
		t.Fatalf("허용되지 않은 역할: %v", err)
	}
	if _, err := s.verifySigner(ctx, "other_plant", userID, "4321", roleAdmin); err != errSignerNotFound {
		t.Fatalf("다른 공장의 계정: %v", err)
	}
}

func TestPlantRouteRequiresProcessingSite(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	_ = s.store.UpsertFarm(ctx, &farm.Farm{FarmID: "f1", Operator: "시험"})
	_ = s.store.UpsertSiteLand(ctx, &site.SiteLand{SiteID: "farm_site", FarmID: "f1", Name: "양식장", Timezone: "Asia/Seoul"})
	for _, siteID := range []string{"farm_site", "no_such"} {
		w := plantDo(t, s, http.MethodPost, "/v1/plant/"+siteID+"/zones", map[string]any{"name": "x"})
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "NOT_PROCESSING_SITE") {
			t.Fatalf("%s: 가공 사업장이 아니면 거부: %d %s", siteID, w.Code, w.Body.String())
		}
	}
}

// 다른 사업장이 쓰는 id 로는 등록할 수 없다 — 남의 구역을 덮어쓰지 않는다.
func TestPlantIDOwnedByOtherSite(t *testing.T) {
	s := newTestServerWithApp(t)
	newProcessingSite(t, s)
	if err := s.store.UpsertSiteProcessing(context.Background(),
		&site.SiteLand{SiteID: "plant_02", FarmID: "farm_01", Name: "둘째 공장", Timezone: "Asia/Seoul"}); err != nil {
		t.Fatalf("second site: %v", err)
	}
	if w := plantDo(t, s, http.MethodPost, "/v1/plant/plant_01/zones", map[string]any{"zone_id": "z1", "name": "첫째 공장 구역"}); w.Code != http.StatusOK {
		t.Fatalf("zone: %d", w.Code)
	}
	w := plantDo(t, s, http.MethodPost, "/v1/plant/plant_02/zones", map[string]any{"zone_id": "z1", "name": "덮어쓰기 시도"})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "ID_TAKEN") {
		t.Fatalf("다른 사업장의 id 는 409: %d %s", w.Code, w.Body.String())
	}
	zones, _ := s.store.ListPlantZones(context.Background(), "plant_01")
	if len(zones) != 1 || zones[0].Name != "첫째 공장 구역" {
		t.Fatalf("원래 구역이 그대로여야 함: %+v", zones)
	}
}
