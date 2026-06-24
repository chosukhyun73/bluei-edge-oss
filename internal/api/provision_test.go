package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

func resetProvision() {
	provisionJobs.mu.Lock()
	provisionJobs.jobs = map[string]*provisionJob{}
	provisionJobs.running = false
	provisionJobs.mu.Unlock()
}

func postProvision(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/v1/provision", bytes.NewReader(b))
	w := httptest.NewRecorder()
	s.handleProvisionStart(w, r)
	return w
}

func TestProvisionPortsEndpoint(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest("GET", "/v1/provision/ports", nil)
	w := httptest.NewRecorder()
	s.handleProvisionPorts(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ports: got %d", w.Code)
	}
	var resp struct {
		Ports []string `json:"ports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	// ports 는 [] 또는 실제 목록 — nil 이면 JSON null 이 되어선 안 됨.
	if resp.Ports == nil {
		t.Fatalf("ports should be a (possibly empty) array, got null")
	}
}

func TestProvisionStartValidation(t *testing.T) {
	resetProvision()
	s := newTestServer(t)

	// tank_id 누락
	if w := postProvision(t, s, map[string]any{"type": "feeder"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing tank: got %d body=%s", w.Code, w.Body.String())
	}
	// 미지원 타입
	if w := postProvision(t, s, map[string]any{"tank_id": "ras_tank_03", "type": "pump"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad type: got %d", w.Code)
	}
	// 잘못된 포트
	if w := postProvision(t, s, map[string]any{"tank_id": "ras_tank_03", "port": "/etc/passwd"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad port: got %d", w.Code)
	}
}

func TestProvisionJobLifecycle(t *testing.T) {
	resetProvision()
	orig := provisionExec
	provisionExec = func(ctx context.Context, args, env []string) *exec.Cmd {
		return exec.CommandContext(ctx, "bash", "-c", "echo provision-stub-log; exit 0")
	}
	defer func() { provisionExec = orig }()

	s := newTestServer(t)
	w := postProvision(t, s, map[string]any{"tank_id": "ras_tank_03"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: got %d body=%s", w.Code, w.Body.String())
	}
	var started struct {
		JobID        string `json:"job_id"`
		ControllerID string `json:"controller_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	if started.JobID == "" {
		t.Fatal("no job_id")
	}
	if started.ControllerID != "feeder_ras_tank_03" {
		t.Fatalf("derived controller_id: %q", started.ControllerID)
	}

	// 폴링 until 완료
	deadline := time.Now().Add(3 * time.Second)
	var snap provisionJob
	for time.Now().Before(deadline) {
		r := httptest.NewRequest("GET", "/v1/provision/"+started.JobID, nil)
		gw := httptest.NewRecorder()
		s.handleProvisionItem(gw, r)
		if gw.Code != http.StatusOK {
			t.Fatalf("item: got %d", gw.Code)
		}
		_ = json.Unmarshal(gw.Body.Bytes(), &snap)
		if snap.Status != "running" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if snap.Status != "success" {
		t.Fatalf("expected success, got %q (exit=%d)", snap.Status, snap.ExitCode)
	}
	if !bytes.Contains([]byte(snap.Log), []byte("provision-stub-log")) {
		t.Fatalf("log not captured: %q", snap.Log)
	}
}

func TestProvisionConcurrentRejected(t *testing.T) {
	resetProvision()
	orig := provisionExec
	provisionExec = func(ctx context.Context, args, env []string) *exec.Cmd {
		return exec.CommandContext(ctx, "bash", "-c", "sleep 0.4; exit 0")
	}
	defer func() { provisionExec = orig }()

	s := newTestServer(t)
	if w := postProvision(t, s, map[string]any{"tank_id": "ras_tank_03"}); w.Code != http.StatusAccepted {
		t.Fatalf("first start: got %d", w.Code)
	}
	// 진행 중 두 번째 시작은 409
	if w := postProvision(t, s, map[string]any{"tank_id": "ras_tank_04"}); w.Code != http.StatusConflict {
		t.Fatalf("second start should be 409, got %d", w.Code)
	}
	// 첫 job 완료 대기 후 running 해제 확인
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provisionJobs.mu.Lock()
		running := provisionJobs.running
		provisionJobs.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("job did not clear running flag")
}
