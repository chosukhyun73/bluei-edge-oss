package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/training"
)

// newTestServerWithTraining builds a test Server with a minimal training service.
func newTestServerWithTraining(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	s.train = training.New(nil, training.Config{
		ScriptPath:   "/nonexistent/fake_train.py",
		PythonBin:    "python3",
		CandidateDir: t.TempDir(),
	})
	return s
}

func postTrainingJob(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/vision/training/jobs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTrainingJobsRoute(w, req)
	return w
}

// TestTrainingJobStartMissingTankID — tank-baseline 에 tank_id 없으면 422 + MISSING_TANK_ID.
func TestTrainingJobStartMissingTankID(t *testing.T) {
	s := newTestServerWithTraining(t)
	w := postTrainingJob(t, s, map[string]any{"kind": "tank-baseline"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if errCode(t, w) != "MISSING_TANK_ID" {
		t.Errorf("expected code MISSING_TANK_ID, got %v", errCode(t, w))
	}
}

// TestTrainingJobStartMissingTankIDForecast — water-forecast 도 동일.
func TestTrainingJobStartMissingTankIDForecast(t *testing.T) {
	s := newTestServerWithTraining(t)
	w := postTrainingJob(t, s, map[string]any{"kind": "water-forecast"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if errCode(t, w) != "MISSING_TANK_ID" {
		t.Errorf("expected code MISSING_TANK_ID, got %v", errCode(t, w))
	}
}

// TestTrainingJobStartInvalidKind — 알 수 없는 kind 는 422 + INVALID_KIND.
func TestTrainingJobStartInvalidKind(t *testing.T) {
	s := newTestServerWithTraining(t)
	w := postTrainingJob(t, s, map[string]any{"kind": "super-ai"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if errCode(t, w) != "INVALID_KIND" {
		t.Errorf("expected code INVALID_KIND, got %v", errCode(t, w))
	}
}

// TestTrainingJobStartVisionMissingAlgorithmID — vision-detector 에 algorithm_id 없으면 422.
func TestTrainingJobStartVisionMissingAlgorithmID(t *testing.T) {
	s := newTestServerWithTraining(t)
	w := postTrainingJob(t, s, map[string]any{"kind": "vision-detector"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if errCode(t, w) != "MISSING_ALGORITHM_ID" {
		t.Errorf("expected code MISSING_ALGORITHM_ID, got %v", errCode(t, w))
	}
}

// errCode extracts {"error": {"code": ...}} from the response body.
func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Error.Code
}

// TestTrainingJobStartDefaultsToVision — kind 없으면 vision-detector 로 취급, algorithm_id 없으면 422.
func TestTrainingJobStartDefaultsToVision(t *testing.T) {
	s := newTestServerWithTraining(t)
	// kind 생략 → vision-detector 기본 → algorithm_id 없으면 422
	w := postTrainingJob(t, s, map[string]any{})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}
