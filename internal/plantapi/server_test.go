package plantapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// newTestStore — 실제 실행과 같은 migration 목록을 임시 DB 에 적용한다.
func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(root, "migrations", "001_init.sql")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	for _, p := range storage.MigrationPaths {
		if err := storage.Migrate(st, filepath.Join(root, p)); err != nil {
			t.Fatalf("migrate %s: %v", p, err)
		}
	}
	return st
}

func newTestServerWithApp(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	store := newTestStore(t)
	return &Server{cfg: cfg, app: runtime.NewApp(cfg, store), store: store}
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	return m
}

func TestPlantRegistration(t *testing.T) {
	s := newTestServerWithApp(t)
	do := func(method string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.handlePlantsCollection(w, httptest.NewRequest(method, "/v1/plants", bytes.NewReader(b)))
		return w
	}
	if w := do(http.MethodGet, nil); decodeMap(t, w)["count"].(float64) != 0 {
		t.Fatalf("처음에는 공장 0건: %s", w.Body.String())
	}
	if w := do(http.MethodPost, map[string]any{"name": " "}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("이름 없는 등록은 422: %d", w.Code)
	}
	w := do(http.MethodPost, map[string]any{"name": "시험 가공공장", "address": "부산"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create plant: %d %s", w.Code, w.Body.String())
	}
	siteID := decodeMap(t, w)["plant"].(map[string]any)["site_id"].(string)
	// 등록한 공장은 가공 사업장이어야 하고, 목록에 나온다
	if typ, _ := s.store.GetSiteType(t.Context(), siteID); typ != "processing" {
		t.Fatalf("site_type = %q, want processing", typ)
	}
	if w := do(http.MethodGet, nil); decodeMap(t, w)["count"].(float64) != 1 {
		t.Fatalf("등록 뒤 공장 1건: %s", w.Body.String())
	}
	// 두 번째 공장은 같은 사업자 아래에 놓인다(사업자를 새로 만들지 않는다)
	do(http.MethodPost, map[string]any{"name": "제2공장"})
	if farms, _ := s.store.ListFarms(t.Context()); len(farms) != 1 {
		t.Fatalf("사업자는 1건이어야 함: %d", len(farms))
	}
}
