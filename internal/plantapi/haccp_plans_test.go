package plantapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/haccp"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/storage"
)

func haccpDo(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	w := httptest.NewRecorder()
	if path == "/v1/haccp/plans" {
		s.handleHACCPPlansCollection(w, req)
	} else {
		s.handleHACCPPlanItemRoute(w, req)
	}
	return w
}

func newProcessingSite(t *testing.T, s *Server) {
	t.Helper()
	if err := s.store.UpsertFarm(context.Background(), &farm.Farm{FarmID: "farm_01", Operator: "시험"}); err != nil {
		t.Fatalf("seed farm: %v", err)
	}
	if err := s.store.UpsertSiteProcessing(context.Background(),
		&site.SiteLand{SiteID: "plant_01", FarmID: "farm_01", Name: "시험 가공공장", Timezone: "Asia/Seoul"}); err != nil {
		t.Fatalf("create processing site: %v", err)
	}
}

func plantDo(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	w := httptest.NewRecorder()
	s.handlePlantRoute(w, req)
	return w
}

// testSigners — 관리자·검토자·담당자 계정을 만들고 user_id 를 돌려준다. PIN 은 역할별로 다르다.
type testSigners struct{ admin, reviewer, monitor string }

const (
	pinAdmin    = "1111"
	pinReviewer = "2222"
	pinMonitor  = "3333"
)

func newSigners(t *testing.T, s *Server) testSigners {
	t.Helper()
	mk := func(name, role, pin string) string {
		w := plantDo(t, s, http.MethodPost, "/v1/plant/plant_01/users", map[string]any{"name": name, "role": role, "pin": pin})
		if w.Code != http.StatusCreated {
			t.Fatalf("create user %s: %d %s", role, w.Code, w.Body.String())
		}
		return decodeMap(t, w)["user"].(map[string]any)["user_id"].(string)
	}
	return testSigners{admin: mk("관리자", "admin", pinAdmin), reviewer: mk("검토자", "reviewer", pinReviewer), monitor: mk("담당자", "monitor", pinMonitor)}
}

func (sg testSigners) publishBody() map[string]any {
	return map[string]any{
		"onsite_confirmed_by": sg.reviewer, "onsite_confirmed_pin": pinReviewer,
		"published_by": sg.admin, "published_pin": pinAdmin,
	}
}

// registerSensors — 계획이 가리키는 센서를 공장에 등록한다(구역 하나에 모의 센서).
func registerSensors(t *testing.T, s *Server, c *haccp.PlanContent) {
	t.Helper()
	if w := plantDo(t, s, http.MethodPost, "/v1/plant/plant_01/zones", map[string]any{"zone_id": "zone_work", "name": "작업장"}); w.Code != http.StatusOK {
		t.Fatalf("create zone: %d %s", w.Code, w.Body.String())
	}
	for _, k := range c.CCPs {
		for _, l := range k.Limits {
			if l.SensorID == "" {
				continue
			}
			w := plantDo(t, s, http.MethodPost, "/v1/plant/plant_01/sensors", map[string]any{
				"sensor_id": l.SensorID, "zone_id": "zone_work", "metric": l.Item, "unit": l.Unit, "is_mock": true,
			})
			if w.Code != http.StatusOK {
				t.Fatalf("create sensor %s: %d %s", l.SensorID, w.Code, w.Body.String())
			}
		}
	}
}

// completeContent — 공장이 채워야 하는 값(제품명·담당·센서·불능 조건·빈 한계값·확인)을 채운다.
func completeContent(c *haccp.PlanContent) {
	c.Product.Name = "냉장 방어 로인"
	for i := range c.Hazards {
		c.Hazards[i].Confirmed = true
	}
	for i := range c.CCPs {
		k := &c.CCPs[i]
		k.MonitorPrimary = "user_monitor"
		k.OutageAfterSec = 600
		for j := range k.Limits {
			l := &k.Limits[j]
			if l.SourceKind == haccp.SourceSensor {
				l.SensorID = "mock_" + l.LimitID
			}
			if l.RuleKind == haccp.RuleInstant && l.Min == nil && l.Max == nil {
				v := 30.0
				l.Max = &v
			}
			if l.BasisRef == "" {
				l.BasisRef = "자사 검증 기록"
			}
			l.Confirmed = true
		}
	}
}

func versionContent(t *testing.T, m map[string]any) haccp.PlanContent {
	t.Helper()
	b, _ := json.Marshal(m["version"].(map[string]any)["content"])
	var c haccp.PlanContent
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	return c
}

func TestHACCPPlanRegisterPublishRevise(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	newProcessingSite(t, s)
	sg := newSigners(t, s)

	// 1) 템플릿에서 계획 생성 → 초안 v1
	w := haccpDo(t, s, http.MethodPost, "/v1/haccp/plans", map[string]any{
		"site_id": "plant_01", "name": "신선 방어", "template_id": "fresh_histamine_fish",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan: %d %s", w.Code, w.Body.String())
	}
	created := decodeMap(t, w)
	planID := created["plan"].(map[string]any)["plan_id"].(string)
	ver := created["version"].(map[string]any)
	v1 := ver["version_id"].(string)
	if ver["status"] != "draft" || ver["version_no"].(float64) != 1 {
		t.Fatalf("첫 버전은 초안 v1 이어야 함: %v", ver)
	}
	if len(ver["publish_issues"].([]any)) == 0 {
		t.Fatal("템플릿 그대로는 게시를 막는 항목이 있어야 함")
	}
	base := "/v1/haccp/plans/" + planID

	// 2) 채우지 않은 초안은 게시 거부
	w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v1+"/publish", sg.publishBody())
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "PUBLISH_BLOCKED") {
		t.Fatalf("게시가 막혀야 함: %d %s", w.Code, w.Body.String())
	}

	// 3) 구조가 틀린 초안은 저장 거부
	content := versionContent(t, created)
	bad := content
	bad.Links = append(append([]haccp.Link{}, content.Links...),
		haccp.Link{LinkID: "bad", From: "cut", To: "nowhere", FlowKind: haccp.FlowProduct})
	w = haccpDo(t, s, http.MethodPut, base+"/versions/"+v1, map[string]any{"content": bad})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("구조 오류 저장은 422: %d %s", w.Code, w.Body.String())
	}

	// 4) 값을 채워 저장 → 서명 없이는 거부 → 서명하면 게시
	completeContent(&content)
	w = haccpDo(t, s, http.MethodPut, base+"/versions/"+v1, map[string]any{"content": content})
	if w.Code != http.StatusOK {
		t.Fatalf("save draft: %d %s", w.Code, w.Body.String())
	}
	if n := len(decodeMap(t, w)["version"].(map[string]any)["publish_issues"].([]any)); n != 0 {
		t.Fatalf("채운 뒤에는 막는 항목이 없어야 함, got %d", n)
	}
	w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v1+"/publish",
		map[string]any{"published_by": sg.admin, "published_pin": pinAdmin})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "MISSING_SIGNATURE") {
		t.Fatalf("현장 확인 서명 없이는 거부: %d %s", w.Code, w.Body.String())
	}
	// 서명 검사: 틀린 PIN, 권한 없는 역할, 없는 계정은 모두 거부
	for name, body := range map[string]map[string]any{
		"틀린 PIN":     {"onsite_confirmed_by": sg.reviewer, "onsite_confirmed_pin": "9999", "published_by": sg.admin, "published_pin": pinAdmin},
		"담당자가 현장 확인": {"onsite_confirmed_by": sg.monitor, "onsite_confirmed_pin": pinMonitor, "published_by": sg.admin, "published_pin": pinAdmin},
		"검토자가 게시":    {"onsite_confirmed_by": sg.reviewer, "onsite_confirmed_pin": pinReviewer, "published_by": sg.reviewer, "published_pin": pinReviewer},
		"없는 계정":      {"onsite_confirmed_by": "user_nobody", "onsite_confirmed_pin": "1111", "published_by": sg.admin, "published_pin": pinAdmin},
	} {
		if w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v1+"/publish", body); w.Code != http.StatusForbidden ||
			!strings.Contains(w.Body.String(), "SIGNATURE_REJECTED") {
			t.Fatalf("%s: 서명 거부(403)여야 함: %d %s", name, w.Code, w.Body.String())
		}
	}
	// 센서를 등록하지 않았으면 게시 거부 — 계획이 가리키는 센서가 공장에 없다
	w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v1+"/publish", sg.publishBody())
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "UNKNOWN_SENSOR") {
		t.Fatalf("등록되지 않은 센서면 게시 거부: %d %s", w.Code, w.Body.String())
	}
	registerSensors(t, s, &content)
	w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v1+"/publish", sg.publishBody())
	if w.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	pub := decodeMap(t, w)["version"].(map[string]any)
	hash1 := pub["content_hash"].(string)
	if pub["status"] != "published" || len(hash1) != 64 || pub["ccp_changed"] != false {
		t.Fatalf("게시 결과가 올바르지 않음: %v", pub)
	}

	// 5) 게시본은 고칠 수 없다 (API)
	w = haccpDo(t, s, http.MethodPut, base+"/versions/"+v1, map[string]any{"content": content})
	if w.Code != http.StatusConflict {
		t.Fatalf("게시본 수정은 409: %d %s", w.Code, w.Body.String())
	}

	// 6) 게시 이벤트가 남는다
	evs, err := s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventHACCPPlanPublished, Limit: 10})
	if err != nil || len(evs) != 1 || !strings.Contains(evs[0].PayloadJSON, hash1) {
		t.Fatalf("게시 이벤트 1건 + 해시 포함이어야 함: err=%v n=%d", err, len(evs))
	}

	// 7) 개정: 사유 없이는 거부 → 새 초안 v2 → 한계기준 변경 → 게시하면 v1 은 폐기, CCP 변경 표시
	w = haccpDo(t, s, http.MethodPost, base+"/versions", map[string]any{})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("개정 사유 없이는 422: %d", w.Code)
	}
	w = haccpDo(t, s, http.MethodPost, base+"/versions", map[string]any{"change_reason": "냉장 보관 한계기준 강화"})
	if w.Code != http.StatusCreated {
		t.Fatalf("new draft: %d %s", w.Code, w.Body.String())
	}
	d2 := decodeMap(t, w)
	v2 := d2["version"].(map[string]any)["version_id"].(string)
	if w2 := haccpDo(t, s, http.MethodPost, base+"/versions", map[string]any{"change_reason": "또"}); w2.Code != http.StatusConflict {
		t.Fatalf("초안이 있으면 새 초안은 409: %d", w2.Code)
	}
	c2 := versionContent(t, d2)
	for i := range c2.CCPs {
		if c2.CCPs[i].CCPID == "ccp_chill_store" {
			v := 3.0
			c2.CCPs[i].Limits[0].Max = &v
		}
	}
	if w = haccpDo(t, s, http.MethodPut, base+"/versions/"+v2, map[string]any{"content": c2}); w.Code != http.StatusOK {
		t.Fatalf("save v2: %d %s", w.Code, w.Body.String())
	}
	w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v2+"/publish", sg.publishBody())
	if w.Code != http.StatusOK {
		t.Fatalf("publish v2: %d %s", w.Code, w.Body.String())
	}
	pub2 := decodeMap(t, w)["version"].(map[string]any)
	if pub2["ccp_changed"] != true || pub2["content_hash"] == hash1 {
		t.Fatalf("v2 는 CCP 변경 표시 + 다른 해시여야 함: %v", pub2)
	}
	w = haccpDo(t, s, http.MethodGet, base, nil)
	vs := decodeMap(t, w)["versions"].([]any)
	if len(vs) != 2 || vs[0].(map[string]any)["status"] != "retired" || vs[1].(map[string]any)["status"] != "published" {
		t.Fatalf("v1 폐기, v2 게시여야 함: %v", vs)
	}
	// v1 내용과 해시는 그대로 남는다
	old, _ := s.store.GetHACCPPlanVersion(ctx, v1)
	if old.ContentHash != hash1 {
		t.Fatal("폐기된 v1 의 해시가 바뀌면 안 됨")
	}
}

// 게시본은 DB 단에서도 바뀌지 않는다 (트리거).
func TestHACCPPublishedVersionImmutableInDB(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	newProcessingSite(t, s)
	sg := newSigners(t, s)
	w := haccpDo(t, s, http.MethodPost, "/v1/haccp/plans", map[string]any{
		"site_id": "plant_01", "name": "냉동 고등어", "template_id": "frozen_fish",
	})
	created := decodeMap(t, w)
	planID := created["plan"].(map[string]any)["plan_id"].(string)
	v1 := created["version"].(map[string]any)["version_id"].(string)
	content := versionContent(t, created)
	completeContent(&content)
	base := "/v1/haccp/plans/" + planID
	registerSensors(t, s, &content)
	haccpDo(t, s, http.MethodPut, base+"/versions/"+v1, map[string]any{"content": content})
	if w = haccpDo(t, s, http.MethodPost, base+"/versions/"+v1+"/publish", sg.publishBody()); w.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}

	// 초안 저장 경로는 게시본을 건드리지 못한다
	ok, err := s.store.UpdateHACCPDraftContent(ctx, v1, `{}`)
	if err != nil || ok {
		t.Fatalf("게시본 내용 저장은 거부되어야 함: ok=%v err=%v", ok, err)
	}
	// 다시 게시(= 내용·해시 덮어쓰기)도 안 된다
	v, _ := s.store.GetHACCPPlanVersion(ctx, v1)
	v.ContentHash = "tampered"
	if ok, _ := s.store.PublishHACCPPlanVersion(ctx, v); ok {
		t.Fatal("게시본을 다시 게시할 수 없어야 함")
	}
	after, _ := s.store.GetHACCPPlanVersion(ctx, v1)
	if after.Status != "published" || after.ContentHash == "tampered" {
		t.Fatalf("게시본이 바뀌면 안 됨: %+v", after)
	}
}

func TestHACCPPlanRequiresProcessingSite(t *testing.T) {
	s := newTestServerWithApp(t)
	if err := s.store.UpsertFarm(context.Background(), &farm.Farm{FarmID: "f1", Operator: "시험"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertSiteLand(context.Background(),
		&site.SiteLand{SiteID: "farm_site", FarmID: "f1", Name: "양식장", Timezone: "Asia/Seoul"}); err != nil {
		t.Fatal(err)
	}
	for _, siteID := range []string{"farm_site", "no_such_site"} {
		w := haccpDo(t, s, http.MethodPost, "/v1/haccp/plans", map[string]any{"site_id": siteID, "name": "x"})
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "NOT_PROCESSING_SITE") {
			t.Fatalf("%s: 가공 사업장이 아니면 거부: %d %s", siteID, w.Code, w.Body.String())
		}
	}
}

func TestHACCPTemplatesEndpoint(t *testing.T) {
	s := newTestServerWithApp(t)
	w := httptest.NewRecorder()
	s.handleHACCPTemplates(w, httptest.NewRequest(http.MethodGet, "/v1/haccp/templates", nil))
	if w.Code != http.StatusOK || decodeMap(t, w)["count"].(float64) != 2 {
		t.Fatalf("템플릿 2종이어야 함: %d %s", w.Code, w.Body.String())
	}
}
