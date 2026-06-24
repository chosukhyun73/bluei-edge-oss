package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

func newTestServerForGroups(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	// newTestStore 는 008_groups.sql 까지 포함 (tank_state_vector_test.go 에 정의).
	return &Server{cfg: cfg, store: newTestStore(t)}
}

// groupsPost — POST /v1/groups helper.
func groupsPost(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleGroupsRoute(w, req)
	return w
}

// groupsGet — GET /v1/groups helper.
func groupsGet(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/groups", nil)
	w := httptest.NewRecorder()
	s.handleGroupsRoute(w, req)
	return w
}

// groupGet — GET /v1/groups/{id} helper.
func groupGet(t *testing.T, s *Server, groupID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID, nil)
	req.SetPathValue("group_id", groupID)
	w := httptest.NewRecorder()
	s.handleGroupRoute(w, req)
	return w
}

// groupPut — PUT /v1/groups/{id} helper.
func groupPut(t *testing.T, s *Server, groupID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/v1/groups/"+groupID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleGroupRoute(w, req)
	return w
}

// groupDelete — DELETE /v1/groups/{id} helper.
func groupDelete(t *testing.T, s *Server, groupID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/v1/groups/"+groupID, nil)
	w := httptest.NewRecorder()
	s.handleGroupRoute(w, req)
	return w
}

// groupTanks — GET /v1/groups/{id}/tanks helper.
func groupTanks(t *testing.T, s *Server, groupID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/tanks", nil)
	w := httptest.NewRecorder()
	s.handleGroupRoute(w, req)
	return w
}

func TestListGroupsEmpty(t *testing.T) {
	s := newTestServerForGroups(t)
	w := groupsGet(t, s)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if count, _ := resp["count"].(float64); count != 0 {
		t.Errorf("expected count=0, got %v", count)
	}
}

func TestCreateGetGroup(t *testing.T) {
	s := newTestServerForGroups(t)

	// 생성
	w := groupsPost(t, s, map[string]any{
		"group_id":    "g1",
		"name":        "A동 순환시스템",
		"description": "광어 전문 양식 그룹",
		"color":       "#22c55e",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// 조회
	w2 := groupGet(t, s, "g1")
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["name"] != "A동 순환시스템" {
		t.Errorf("name: got %v", resp["name"])
	}
	if resp["color"] != "#22c55e" {
		t.Errorf("color: got %v", resp["color"])
	}

	// 없는 group → 404
	w3 := groupGet(t, s, "g_missing")
	if w3.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing, got %d", w3.Code)
	}
}

func TestUpdateGroup(t *testing.T) {
	s := newTestServerForGroups(t)

	groupsPost(t, s, map[string]any{
		"group_id": "g1",
		"name":     "A동 순환시스템",
		"color":    "#22c55e",
	})

	w := groupPut(t, s, "g1", map[string]any{
		"group_id": "g1",
		"name":     "A동 순환시스템 (수정)",
		"color":    "#22c55e",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w2 := groupGet(t, s, "g1")
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["name"] != "A동 순환시스템 (수정)" {
		t.Errorf("update: name=%v", resp["name"])
	}
}

func TestDeleteGroupBlockedByTanks(t *testing.T) {
	s := newTestServerForGroups(t)

	// group 생성
	groupsPost(t, s, map[string]any{
		"group_id": "g1",
		"name":     "A동",
		"color":    "#22c55e",
	})

	// Cage/Tank 생성 — group_id=g1
	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	if err := s.store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "t1",
		DisplayName: "Tank 1",
		Species:     "halibut",
		SystemType:  "ras",
		GroupID:     "g1",
	}); err != nil {
		t.Fatalf("UpsertTankProfile: %v", err)
	}

	// DELETE → 409 (Cage/Tank 가 참조 중)
	w := groupDelete(t, s, "g1")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errBody, _ := resp["error"].(map[string]any)
	if errBody["code"] != "GROUP_HAS_TANKS" {
		t.Errorf("expected GROUP_HAS_TANKS, got %v", errBody["code"])
	}
}

func TestDeleteGroupOk(t *testing.T) {
	s := newTestServerForGroups(t)

	groupsPost(t, s, map[string]any{
		"group_id": "g1",
		"name":     "A동",
		"color":    "#22c55e",
	})

	// Cage/Tank 없이 삭제 → 200
	w := groupDelete(t, s, "g1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 이후 조회 → 404
	w2 := groupGet(t, s, "g1")
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w2.Code)
	}
}

func TestListTanksByGroupAPI(t *testing.T) {
	s := newTestServerForGroups(t)

	groupsPost(t, s, map[string]any{
		"group_id": "g1",
		"name":     "A동",
		"color":    "#22c55e",
	})
	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	for _, tp := range []*storage.TankProfile{
		{TankID: "t1", DisplayName: "T1", Species: "halibut", SystemType: "ras", GroupID: "g1"},
		{TankID: "t2", DisplayName: "T2", Species: "halibut", SystemType: "ras", GroupID: "g1"},
	} {
		if err := s.store.UpsertTankProfile(ctx, tp); err != nil {
			t.Fatalf("UpsertTankProfile %s: %v", tp.TankID, err)
		}
	}

	w := groupTanks(t, s, "g1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if count, _ := resp["count"].(float64); count != 2 {
		t.Errorf("expected count=2, got %v", count)
	}

	// 없는 group → 404
	w2 := groupTanks(t, s, "g_missing")
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing group, got %d", w2.Code)
	}
}

func TestCreateInvalidColor(t *testing.T) {
	s := newTestServerForGroups(t)

	w := groupsPost(t, s, map[string]any{
		"group_id": "g1",
		"name":     "A동",
		"color":    "green", // HEX 형식 아님
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errBody, _ := resp["error"].(map[string]any)
	if errBody["code"] != "INVALID_GROUP_BODY" {
		t.Errorf("expected INVALID_GROUP_BODY, got %v", errBody["code"])
	}
}
