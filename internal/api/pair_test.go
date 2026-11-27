package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/config"
)

func newPairTestServer(t *testing.T, authEnabled bool) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Edge.DataDir = t.TempDir()
	cfg.Sync.NodeCode = "gx10-test0001"
	cfg.Sync.AccessToken = "gx10_pair-token-for-test"
	cfg.API.Auth.Enabled = authEnabled
	cfg.API.Auth.OperatorTokenEnv = "BLUEI_EDGE_OPERATOR_TOKEN_PAIR_TEST"
	t.Setenv("BLUEI_EDGE_OPERATOR_TOKEN_PAIR_TEST", "op-secret")
	return &Server{cfg: cfg}
}

func getPair(s *Server, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/pair", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	s.authMiddleware(s.handlePair)(w, req)
	return w
}

func TestPairRequiresOperatorTokenWhenAuthEnabled(t *testing.T) {
	s := newPairTestServer(t, true)
	if w := getPair(s, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", w.Code)
	}
	if w := getPair(s, "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: want 401, got %d", w.Code)
	}
	w := getPair(s, "op-secret")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusOK || body["access_token"] != "gx10_pair-token-for-test" {
		t.Fatalf("operator: want 200 with pair token, got %d %v", w.Code, body)
	}
}

func TestPairOmitsTokenAfterDeviceAuth(t *testing.T) {
	s := newPairTestServer(t, false)
	if err := config.SaveDeviceCreds(s.cfg.Edge.DataDir, config.DeviceCreds{
		NodeCode: "gx10-test0001", AccessToken: "gx10_live-device-token",
	}); err != nil {
		t.Fatal(err)
	}
	w := getPair(s, "")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["access_token"]; ok || body["paired"] != true {
		t.Fatalf("paired device must not expose token: %v", body)
	}
}
