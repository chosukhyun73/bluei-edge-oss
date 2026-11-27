package plantapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/haccp"
)

func TestPlantCertificates(t *testing.T) {
	s := newTestServerWithApp(t)
	newProcessingSite(t, s)
	base := "/v1/plant/plant_01/certificates"

	bad := []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"scheme": "asc_coc"}, "INVALID_CERTIFICATE"},
		{map[string]any{"scheme": "unknown", "cert_no": "X-1"}, "INVALID_CERTIFICATE"},
		{map[string]any{"scheme": "msc_coc", "cert_no": "X-1", "holder_kind": "supplier"}, "MISSING_HOLDER_NAME"},
		{map[string]any{"scheme": "msc_coc", "cert_no": "X-1", "valid_until": "2027/01/01"}, "INVALID_DATE"},
		{map[string]any{"scheme": "msc_coc", "cert_no": "X-1", "valid_from": "2027-01-01", "valid_until": "2026-01-01"}, "INVALID_DATE"},
	}
	for _, c := range bad {
		if w := plantDo(t, s, http.MethodPost, base, c.body); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), c.code) {
			t.Fatalf("%v 는 %s 로 거부되어야 함: %d %s", c.body, c.code, w.Code, w.Body.String())
		}
	}

	w := plantDo(t, s, http.MethodPost, base, map[string]any{"cert_id": "cert_old", "scheme": "asc_coc", "cert_no": "ASC-C-0001", "valid_until": "2020-12-31"})
	if w.Code != http.StatusOK || decodeMap(t, w)["certificate"].(map[string]any)["expired"] != true {
		t.Fatalf("만료일이 지난 인증서는 expired=true: %d %s", w.Code, w.Body.String())
	}
	// 같은 id 로 다시 보내면 갱신
	w = plantDo(t, s, http.MethodPost, base, map[string]any{"cert_id": "cert_old", "scheme": "asc_coc", "cert_no": "ASC-C-0001", "valid_until": "2099-12-31"})
	if w.Code != http.StatusOK || decodeMap(t, w)["certificate"].(map[string]any)["expired"] != false {
		t.Fatalf("갱신 뒤 expired=false: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handlePlantRoute(w, httptest.NewRequest(http.MethodGet, base, nil))
	if got := decodeMap(t, w)["count"].(float64); got != 1 {
		t.Fatalf("원장 1건이어야 함: %v", got)
	}
}

// complianceItem returns the status of one item in the plant's compliance view.
func complianceItem(t *testing.T, m map[string]any, profile, item string) string {
	t.Helper()
	for _, p := range m["profiles"].([]any) {
		pm := p.(map[string]any)
		if pm["id"] != profile {
			continue
		}
		for _, it := range pm["items"].([]any) {
			im := it.(map[string]any)
			if im["id"] == item {
				return im["status"].(string)
			}
		}
	}
	t.Fatalf("no item %s/%s in %v", profile, item, m)
	return ""
}

func TestPlantCompliance(t *testing.T) {
	s := newTestServerWithApp(t)
	newProcessingSite(t, s)
	base := "/v1/plant/plant_01"
	get := func() map[string]any {
		w := httptest.NewRecorder()
		s.handlePlantRoute(w, httptest.NewRequest(http.MethodGet, base+"/compliance", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("get compliance: %d %s", w.Code, w.Body.String())
		}
		return decodeMap(t, w)
	}

	if m := get(); len(m["profiles"].([]any)) != 0 {
		t.Fatalf("고르기 전에는 제도 0건: %v", m)
	}
	if w := plantDo(t, s, http.MethodPut, base+"/compliance", map[string]any{"profile_ids": []string{"nope"}}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("없는 제도는 422: %d", w.Code)
	}
	w := plantDo(t, s, http.MethodPut, base+"/compliance", map[string]any{"profile_ids": []string{"kr_haccp", "gdst", "asc_coc"}})
	if w.Code != http.StatusOK {
		t.Fatalf("put compliance: %d %s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	for _, c := range [][2]string{{"gdst", "license"}, {"gdst", "welfare"}, {"kr_haccp", "plan"}, {"kr_haccp", "review"}, {"asc_coc", "cert"}, {"asc_coc", "food_safety"}} {
		if got := complianceItem(t, m, c[0], c[1]); got != "unmet" {
			t.Errorf("빈 공장의 %s/%s 는 unmet 이어야 함: %s", c[0], c[1], got)
		}
	}
	if got := complianceItem(t, m, "gdst", "events"); got != "later" {
		t.Errorf("실행 기록 항목은 later: %s", got)
	}
	if r := m["retention"].(map[string]any); r["required_years"].(float64) != 3 || r["ok"] != true {
		t.Errorf("ASC 를 고르면 보관 3년 필요, 기본 3년으로 충족: %v", r)
	}

	// 보관기간을 2년으로 줄이면 ASC 기준에 못 미친다
	plantDo(t, s, http.MethodPut, base+"/profile", map[string]any{
		"license_name": "수산물가공업 신고", "license_no": "제2026-0001호", "license_authority": "부산광역시 사하구",
		"human_welfare_policy": "internal_policy", "retention_years": 2,
	})
	// 한국 HACCP 인증서만으로는 ASC 식품안전 항목이 채워지지 않는다. 만료된 ASC 인증서도 마찬가지
	plantDo(t, s, http.MethodPost, base+"/certificates", map[string]any{"scheme": "haccp_kr", "cert_no": "H-1"})
	plantDo(t, s, http.MethodPost, base+"/certificates", map[string]any{"cert_id": "cert_asc", "scheme": "asc_coc", "cert_no": "ASC-1", "valid_until": "2020-01-01"})
	// 공급자의 ISO 22000 은 자사 요건을 채우지 않는다
	plantDo(t, s, http.MethodPost, base+"/certificates", map[string]any{"scheme": "iso22000", "cert_no": "I-1", "holder_kind": "supplier", "holder_name": "공급사"})
	m = get()
	want := map[[2]string]string{
		{"gdst", "license"}: "met", {"gdst", "welfare"}: "met", {"kr_haccp", "cert"}: "met",
		{"kr_haccp", "retention"}: "met", {"asc_coc", "retention"}: "unmet",
		{"asc_coc", "cert"}: "unmet", {"asc_coc", "food_safety"}: "unmet",
	}
	for k, v := range want {
		if got := complianceItem(t, m, k[0], k[1]); got != v {
			t.Errorf("%s/%s = %s, want %s", k[0], k[1], got, v)
		}
	}
	if r := m["retention"].(map[string]any); r["ok"] != false {
		t.Errorf("보관 2년은 ASC 3년에 못 미침: %v", r)
	}

	// 인증서를 갱신·추가하고 계획을 게시하면 충족으로 바뀐다
	plantDo(t, s, http.MethodPost, base+"/certificates", map[string]any{"cert_id": "cert_asc", "scheme": "asc_coc", "cert_no": "ASC-1", "valid_until": "2099-01-01"})
	plantDo(t, s, http.MethodPost, base+"/certificates", map[string]any{"scheme": "iso22000", "cert_no": "I-2"})
	sg := newSigners(t, s)
	pw := haccpDo(t, s, http.MethodPost, "/v1/haccp/plans", map[string]any{"site_id": "plant_01", "name": "방어", "template_id": "fresh_histamine_fish"})
	if pw.Code != http.StatusCreated {
		t.Fatalf("create plan: %d %s", pw.Code, pw.Body.String())
	}
	pm := decodeMap(t, pw)
	planID := pm["plan"].(map[string]any)["plan_id"].(string)
	versionID := pm["version"].(map[string]any)["version_id"].(string)
	content := versionContent(t, pm)
	completeContent(&content)
	registerSensors(t, s, &content)
	vpath := "/v1/haccp/plans/" + planID + "/versions/" + versionID
	if w := haccpDo(t, s, http.MethodPut, vpath, map[string]any{"content": content}); w.Code != http.StatusOK {
		t.Fatalf("save draft: %d %s", w.Code, w.Body.String())
	}
	if w := haccpDo(t, s, http.MethodPost, vpath+"/publish", sg.publishBody()); w.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	m = get()
	for _, c := range [][2]string{{"kr_haccp", "plan"}, {"kr_haccp", "review"}, {"asc_coc", "cert"}, {"asc_coc", "food_safety"}} {
		if got := complianceItem(t, m, c[0], c[1]); got != "met" {
			t.Errorf("%s/%s 는 met 이어야 함: %s", c[0], c[1], got)
		}
	}
	// 스마트해썹 항목: 모든 CCP 에 수기가 아닌 항목이 있는지는 계획 내용에서 읽는다
	auto := true
	for _, k := range content.CCPs {
		has := false
		for _, l := range k.Limits {
			if l.AutomationLevel != "" && l.AutomationLevel != haccp.AutoManual {
				has = true
			}
		}
		auto = auto && has
	}
	wantSmart := "unmet"
	if auto {
		wantSmart = "met"
	}
	if got := complianceItem(t, m, "kr_haccp", "smart"); got != wantSmart {
		t.Errorf("kr_haccp/smart = %s, want %s", got, wantSmart)
	}
}

func TestComplianceProfilesEndpoint(t *testing.T) {
	s := newTestServerWithApp(t)
	w := httptest.NewRecorder()
	s.handleComplianceProfiles(w, httptest.NewRequest(http.MethodGet, "/v1/compliance/profiles", nil))
	if w.Code != http.StatusOK || decodeMap(t, w)["count"].(float64) != 7 {
		t.Fatalf("프로파일 7종: %d %s", w.Code, w.Body.String())
	}
}
