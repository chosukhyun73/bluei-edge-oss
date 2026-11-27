package plantapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"bluei.kr/edge/internal/storage"
)

func lotDo(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if strings.HasPrefix(path, "/v1/process/lots/") {
		s.handleLotRoute(w, req)
	} else {
		s.handleLotsCollection(w, req)
	}
	return w
}

func ccpStatus(t *testing.T, m map[string]any, code string) string {
	t.Helper()
	for _, k := range m["ccps"].([]any) {
		if km := k.(map[string]any); km["code"] == code {
			return km["status"].(string)
		}
	}
	t.Fatalf("no ccp %s", code)
	return ""
}

// 예시 공장으로 lot 하나를 처음부터 끝까지 돌린다: 시작 → 단계 이동 → 센서 이탈 → 개선조치 → 마감 → 검토.
func TestProductionLotFlow(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	siteID, err := s.seedDemoPlant(ctx)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	users, _ := s.store.ListLocalUsers(ctx, siteID)
	uid := map[string]string{}
	for _, u := range users {
		uid[u.Role] = u.UserID
	}
	plans, _ := s.store.ListHACCPPlans(ctx, siteID)
	sensors, _ := s.store.ListPlantSensors(ctx, siteID)
	var snRaw string
	for _, sn := range sensors {
		if strings.HasPrefix(sn.SensorID, "temp_cold_raw") {
			snRaw = sn.SensorID
		}
	}
	emit := func(sensorID string, v float64) {
		if err := s.store.InsertProcessReading(ctx, siteID, &storage.ProcessReading{SensorID: sensorID, Value: v, Source: "mock", At: nowMs()}); err != nil {
			t.Fatal(err)
		}
	}

	start := map[string]any{"site_id": siteID, "plan_id": plans[0].PlanID, "started_by": uid["monitor"], "pin": "3333",
		"inputs": []map[string]any{{"supplier": "예시 어장", "supplier_lot": "FARM-LOT-1", "species": "방어", "qty_kg": 100}}}
	bad := map[string]any{"site_id": siteID, "plan_id": plans[0].PlanID, "started_by": uid["monitor"], "pin": "0000", "inputs": start["inputs"]}
	if w := lotDo(t, s, http.MethodPost, "/v1/process/lots", bad); w.Code != http.StatusForbidden {
		t.Fatalf("틀린 PIN 으로 시작은 403: %d %s", w.Code, w.Body.String())
	}
	w := lotDo(t, s, http.MethodPost, "/v1/process/lots", start)
	if w.Code != http.StatusCreated {
		t.Fatalf("start lot: %d %s", w.Code, w.Body.String())
	}
	lotID := decodeMap(t, w)["lot"].(map[string]any)["lot_id"].(string)
	path := "/v1/process/lots/" + lotID
	get := func() map[string]any {
		w := lotDo(t, s, http.MethodGet, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("get lot: %d %s", w.Code, w.Body.String())
		}
		return decodeMap(t, w)
	}
	move := func(m map[string]any) map[string]any {
		t.Helper()
		next := m["next"].([]any)
		if len(next) != 1 {
			t.Fatalf("다음 단계가 하나여야 함: %v", next)
		}
		n := next[0].(map[string]any)
		action := "advance"
		if n["tracked"] == true {
			action = "exit"
		}
		w := lotDo(t, s, http.MethodPost, path+"/move", map[string]any{"action": action, "link_id": n["link_id"]})
		if w.Code != http.StatusOK {
			t.Fatalf("move: %d %s", w.Code, w.Body.String())
		}
		m = decodeMap(t, w)
		if action == "exit" { // 이동 시간을 재는 연결: 도착을 따로 찍는다
			w = lotDo(t, s, http.MethodPost, path+"/move", map[string]any{"action": "enter"})
			if w.Code != http.StatusOK {
				t.Fatalf("enter: %d %s", w.Code, w.Body.String())
			}
			m = decodeMap(t, w)
		}
		return m
	}

	// 1) 입고: 사람이 넣는 중심온도 — 입력 전에는 "입력 필요"
	m := get()
	if got := ccpStatus(t, m, "CCP-1C"); got != "needs_input" {
		t.Fatalf("입고 CCP 는 입력 필요: %s", got)
	}
	if w := lotDo(t, s, http.MethodPost, path+"/close", map[string]any{"output_kg": 80, "by": uid["monitor"], "pin": "3333"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("마지막 단계가 아니면 마감 불가: %d", w.Code)
	}
	w = lotDo(t, s, http.MethodPost, path+"/checks", map[string]any{"limit_id": "lim_receive_core", "value": 2.8, "by": uid["monitor"], "pin": "3333"})
	if w.Code != http.StatusOK || ccpStatus(t, decodeMap(t, w), "CCP-1C") != "ok" {
		t.Fatalf("중심온도 2.8℃ 입력 뒤 ok: %d %s", w.Code, w.Body.String())
	}

	// 2) 원료 냉장 보관: 센서 값이 한계(4.4℃)를 넘으면 이탈이 기록된다
	m = move(m)
	emit(snRaw, 3.0)
	if got := ccpStatus(t, get(), "CCP-2C"); got != "ok" {
		t.Fatalf("3.0℃ 는 ok: %s", got)
	}
	emit(snRaw, 6.5)
	m = get()
	if got := ccpStatus(t, m, "CCP-2C"); got != "deviation" {
		t.Fatalf("6.5℃ 는 이탈: %s", got)
	}
	devs := m["deviations"].([]any)
	if len(devs) != 1 || devs[0].(map[string]any)["status"] != "open" {
		t.Fatalf("열린 이탈 1건이어야 함: %v", devs)
	}
	get() // 다시 판정해도 같은 이탈이 또 생기지 않는다
	if n := len(get()["deviations"].([]any)); n != 1 {
		t.Fatalf("이탈은 한 번만 기록: %d", n)
	}
	devPath := path + "/deviations/" + devs[0].(map[string]any)["dev_id"].(string) + "/close"
	if w := lotDo(t, s, http.MethodPost, devPath, map[string]any{"corrective_action": "냉장고 문 닫음", "by": uid["monitor"], "pin": "3333"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("제품 처분 없이 닫기는 422: %d", w.Code)
	}

	// 3) 끝까지 이동. 열린 이탈이 있으면 마감할 수 없다
	// 가공실·완제품 냉장고 센서가 값을 내고 있다. 단계를 빨리 지나가도 진입 직전 값으로 판정된다
	for _, sn := range sensors {
		if !strings.HasPrefix(sn.SensorID, "temp_cold_raw") {
			emit(sn.SensorID, 3.0)
		}
	}
	for i := 0; i < 4; i++ {
		m = move(m)
	}
	if len(m["next"].([]any)) != 0 {
		t.Fatalf("마지막 단계여야 함: %v", m["position"])
	}
	for _, code := range []string{"CCP-3C", "CCP-4C", "CCP-5C"} {
		if got := ccpStatus(t, m, code); got != "ok" {
			t.Fatalf("%s 는 ok 여야 함: %s", code, got)
		}
	}
	closeBody := map[string]any{"output_kg": 62.5, "by": uid["monitor"], "pin": "3333"}
	if w := lotDo(t, s, http.MethodPost, path+"/close", closeBody); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "OPEN_DEVIATIONS") {
		t.Fatalf("열린 이탈이 있으면 마감 불가: %d %s", w.Code, w.Body.String())
	}
	if w := lotDo(t, s, http.MethodPost, devPath, map[string]any{"corrective_action": "냉장고 문 닫고 온도 복구 확인",
		"disposition": "누적 노출 평가 후 정상 출하", "by": uid["monitor"], "pin": "3333"}); w.Code != http.StatusOK {
		t.Fatalf("close deviation: %d %s", w.Code, w.Body.String())
	}
	w = lotDo(t, s, http.MethodPost, path+"/close", closeBody)
	if w.Code != http.StatusOK {
		t.Fatalf("close lot: %d %s", w.Code, w.Body.String())
	}
	lot := decodeMap(t, w)["lot"].(map[string]any)
	if lot["status"] != "closed" || lot["yield_pct"].(float64) != 62.5 {
		t.Fatalf("마감·수율 62.5%%: %v", lot)
	}

	// 4) 검토: 마감한 사람은 자기 기록을 검토할 수 없다. 담당자 역할도 안 된다
	if w := lotDo(t, s, http.MethodPost, path+"/review", map[string]any{"by": uid["monitor"], "pin": "3333"}); w.Code == http.StatusOK {
		t.Fatalf("마감한 담당자가 검토하면 안 됨")
	}
	if w := lotDo(t, s, http.MethodPost, path+"/review", map[string]any{"by": uid["reviewer"], "pin": "2222"}); w.Code != http.StatusOK {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	// 5) 문서 발행: 계획서와 lot 기록서. 같은 내용을 다시 발행하면 기존 발행본을 돌려준다
	s.cfg.Edge.DataDir = t.TempDir()
	issue := func(kind, ref string) map[string]any {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"kind": kind, "ref_id": ref, "by": uid["reviewer"], "pin": "2222"})
		w := httptest.NewRecorder()
		s.handleDocumentsCollection(w, httptest.NewRequest(http.MethodPost, "/v1/documents", bytes.NewReader(b)))
		if strings.Contains(w.Body.String(), "NO_KOREAN_FONT") {
			t.Skip("한글 글꼴이 없는 환경")
		}
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("issue %s: %d %s", kind, w.Code, w.Body.String())
		}
		return decodeMap(t, w)
	}
	lotDocRes := issue("lot", lotID)
	if lotDocRes["document"].(map[string]any)["intact"] != true {
		t.Fatalf("발행 직후 파일 해시가 대장과 같아야 함: %v", lotDocRes)
	}
	if again := issue("lot", lotID); again["reissued"] != true ||
		again["document"].(map[string]any)["doc_id"] != lotDocRes["document"].(map[string]any)["doc_id"] {
		t.Fatalf("같은 내용 재발행은 기존 발행본: %v", again)
	}
	versions, _ := s.store.ListHACCPPlanVersions(ctx, plans[0].PlanID)
	issue("plan", versions[0].VersionID)
	// 보관 중인 파일이 바뀌면 대조에서 드러난다
	docs, _ := s.store.ListIssuedDocuments(ctx, siteID)
	if len(docs) != 2 {
		t.Fatalf("발행 대장 2건: %d", len(docs))
	}
	if err := os.WriteFile(docs[0].Path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.handleDocumentsCollection(w, httptest.NewRequest(http.MethodGet, "/v1/documents?site_id="+siteID, nil))
	intact := 0
	for _, d := range decodeMap(t, w)["documents"].([]any) {
		if d.(map[string]any)["intact"] == true {
			intact++
		}
	}
	if intact != 1 {
		t.Fatalf("바뀐 파일 1건은 대조 불일치여야 함: intact=%d", intact)
	}

	// 마감 뒤에는 움직일 수 없다
	if w := lotDo(t, s, http.MethodPost, path+"/move", map[string]any{"action": "enter"}); w.Code != http.StatusConflict {
		t.Fatalf("마감된 lot 이동은 409: %d", w.Code)
	}
}

// 센서 값이 끊기면 모니터링 불능이 기록된다. 모의 센서는 값을 지정하거나 끊을 수 있다.
func TestMockSensorAndOutage(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	siteID, err := s.seedDemoPlant(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.emitMockReadings(ctx); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handlePlantRoute(w, httptest.NewRequest(http.MethodGet, "/v1/plant/"+siteID+"/readings", nil))
	sensors := decodeMap(t, w)["sensors"].([]any)
	if len(sensors) != 3 {
		t.Fatalf("예시 공장 센서 3개: %d", len(sensors))
	}
	for _, sn := range sensors {
		m := sn.(map[string]any)
		latest, ok := m["latest"].(map[string]any)
		if !ok {
			t.Fatalf("모의 센서가 값을 내야 함: %v", m)
		}
		// 냉장고 센서의 정상 값은 한계(4.4℃) 안이어야 한다
		if strings.Contains(m["sensor_id"].(string), "cold") && latest["value"].(float64) > 4.4 {
			t.Errorf("%s 정상 값이 한계를 넘음: %v", m["sensor_id"], latest["value"])
		}
	}
	// 값 지정 → 다음 측정부터 그 값. 끊김 → 값이 안 들어옴
	id := sensors[0].(map[string]any)["sensor_id"].(string)
	if w := plantDo(t, s, http.MethodPost, "/v1/plant/"+siteID+"/readings", map[string]any{"sensor_id": id, "mode": "fixed", "value": 9.9}); w.Code != http.StatusOK {
		t.Fatalf("mock fixed: %d %s", w.Code, w.Body.String())
	}
	_ = s.emitMockReadings(ctx)
	if r, _ := s.store.LatestProcessReading(ctx, id); r == nil || r.Value != 9.9 {
		t.Fatalf("지정한 값 9.9 가 나와야 함: %+v", r)
	}
	plantDo(t, s, http.MethodPost, "/v1/plant/"+siteID+"/readings", map[string]any{"sensor_id": id, "mode": "outage"})
	before, _ := s.store.ListProcessReadings(ctx, id, 0, nowMs()+1000)
	_ = s.emitMockReadings(ctx)
	after, _ := s.store.ListProcessReadings(ctx, id, 0, nowMs()+1000)
	if len(after) != len(before) {
		t.Fatalf("끊김 중에는 값이 늘지 않아야 함: %d → %d", len(before), len(after))
	}
	if w := plantDo(t, s, http.MethodPost, "/v1/plant/"+siteID+"/readings", map[string]any{"sensor_id": "nope", "mode": "fixed"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("없는 센서는 422: %d", w.Code)
	}
}
