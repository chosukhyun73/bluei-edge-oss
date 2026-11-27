package plantapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/plantdoc"
	"bluei.kr/edge/internal/plantform"
)

// 기본 양식 받기 → 목록 → 단건 → 빈 양식 PDF.
func TestPlantFormsAdoptAndBlankPDF(t *testing.T) {
	s := newTestServerWithApp(t)
	call := func(h http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(method, path, bytes.NewReader(b)))
		return w
	}
	w := call(s.handlePlantsCollection, http.MethodPost, "/v1/plants", map[string]any{"name": "시험 공장"})
	site := decodeMap(t, w)["plant"].(map[string]any)["site_id"].(string)
	base := "/v1/plant/" + site + "/forms"

	if m := decodeMap(t, call(s.handlePlantRoute, http.MethodGet, base, nil)); m["count"].(float64) != 0 {
		t.Fatalf("처음에는 양식이 없어야 한다: %v", m)
	}
	// 두 개만 골라 받는다.
	m := decodeMap(t, call(s.handlePlantRoute, http.MethodPost, base,
		map[string]any{"pack": "kr-small", "form_ids": []string{"daily_hygiene", "ccp_metal"}}))
	if got := m["adopted"].([]any); len(got) != 2 {
		t.Fatalf("adopted = %v", m)
	}
	// 나머지를 전부 받는다 — 이미 있는 두 개는 건드리지 않는다.
	m = decodeMap(t, call(s.handlePlantRoute, http.MethodPost, base, map[string]any{"pack": "kr-small"}))
	pack, _ := plantform.LoadPack("kr-small")
	if len(m["skipped"].([]any)) != 2 || len(m["adopted"].([]any)) != len(pack.Forms)-2 {
		t.Fatalf("두 번째 받기: %v", m)
	}
	if m := decodeMap(t, call(s.handlePlantRoute, http.MethodGet, base, nil)); int(m["count"].(float64)) != len(pack.Forms) {
		t.Fatalf("목록 수 %v, 기대 %d", m["count"], len(pack.Forms))
	}
	m = decodeMap(t, call(s.handlePlantRoute, http.MethodGet, base+"/daily_hygiene", nil))
	if m["revision"].(float64) != 1 || m["source"] != "default" || m["form"].(map[string]any)["kind"] != "checklist" {
		t.Fatalf("단건: %v", m)
	}
	if w := call(s.handlePlantRoute, http.MethodPost, base, map[string]any{"pack": "kr-small", "form_ids": []string{"nope"}}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("없는 양식: %d", w.Code)
	}
	if w := call(s.handlePlantRoute, http.MethodGet, base+"/nope", nil); w.Code != http.StatusNotFound {
		t.Fatalf("없는 양식 조회: %d", w.Code)
	}

	// 양식 종류마다 빈 양식 문서가 만들어진다.
	for _, id := range []string{"daily_hygiene", "ccp_metal", "training_plan", "complaint_record"} {
		pf, err := s.store.GetPlantForm(t.Context(), site, id)
		if err != nil || pf == nil {
			t.Fatalf("%s: %v", id, err)
		}
		doc, err := s.blankFormDoc(t.Context(), pf)
		if err != nil || len(doc.Blocks) < 3 {
			t.Fatalf("%s: 문서 구성 실패 %v", id, err)
		}
		b, err := plantdoc.Render(doc)
		if errors.Is(err, plantdoc.ErrNoFont) {
			t.Skip("한글 글꼴이 없는 환경")
		}
		if err != nil || !bytes.HasPrefix(b, []byte("%PDF")) {
			t.Fatalf("%s: PDF 실패 %v", id, err)
		}
	}
	if w := call(s.handlePlantRoute, http.MethodGet, base+"/ccp_metal/blank.pdf", nil); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("blank.pdf: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
}
