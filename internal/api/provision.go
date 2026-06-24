package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"bluei.kr/edge/internal/common"
)

// ──────────────────────────────────────────────────────────────────────────────
// ESP32 provisioning — 대시보드에서 USB 보드를 flash + 등록까지 자동화.
// 실제 작업은 scripts/provision-esp32.sh 비대화형 모드를 host 에서 spawn 해 수행하고,
// 진행 로그/상태를 in-memory job 으로 노출한다 (프론트가 폴링).
//
// secrets.h 는 단일 공유 파일이라 동시 provisioning 은 불가 — 한 번에 한 job 만 허용.
// ──────────────────────────────────────────────────────────────────────────────

var (
	provisionIDRe   = regexp.MustCompile(`^[a-z0-9_\-]{1,64}$`)
	provisionPortRe = regexp.MustCompile(`^/dev/tty(USB|ACM)[0-9]+$`)
	// 현재 feeder 펌웨어만 존재. 펌프/UV 등은 펌웨어 준비 후 추가.
	provisionTypes = map[string]bool{"feeder": true}
)

type provisionJob struct {
	ID           string `json:"job_id"`
	Status       string `json:"status"` // running | success | flashed_unconfirmed | failed
	ExitCode     int    `json:"exit_code"`
	ControllerID string `json:"controller_id"`
	TankID       string `json:"tank_id"`
	Log          string `json:"log"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
}

type provisionStore struct {
	mu      sync.Mutex
	jobs    map[string]*provisionJob
	running bool
}

var provisionJobs = &provisionStore{jobs: map[string]*provisionJob{}}

// provisionExec builds the command that runs the wizard. Overridable in tests.
var provisionExec = func(ctx context.Context, args, env []string) *exec.Cmd {
	script, _ := filepath.Abs("scripts/provision-esp32.sh")
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	cmd.Env = env
	return cmd
}

type provisionStartRequest struct {
	TankID       string `json:"tank_id"`
	Type         string `json:"type"`
	ControllerID string `json:"controller_id"`
	Port         string `json:"port"`
	SiteID       string `json:"site_id"`
	Reclaim      bool   `json:"reclaim"`
	Activate     bool   `json:"activate"`
}

// GET /v1/provision/ports — USB 시리얼 포트 목록.
func (s *Server) handleProvisionPorts(w http.ResponseWriter, r *http.Request) {
	var ports []string
	for _, g := range []string{"/dev/ttyUSB*", "/dev/ttyACM*"} {
		m, _ := filepath.Glob(g)
		ports = append(ports, m...)
	}
	sort.Strings(ports)
	if ports == nil {
		ports = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ports": ports})
}

// POST /v1/provision — provisioning job 시작.
func (s *Server) handleProvisionStart(w http.ResponseWriter, r *http.Request) {
	var req provisionStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.Type == "" {
		req.Type = "feeder"
	}
	if !provisionTypes[req.Type] {
		writeError(w, http.StatusUnprocessableEntity, "UNSUPPORTED_TYPE",
			"현재 '"+req.Type+"' 타입 펌웨어가 없습니다 (feeder 만 지원).", "")
		return
	}
	if !provisionIDRe.MatchString(req.TankID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_TANK_ID", "tank_id 가 필요합니다 (소문자/숫자/_/-).", "")
		return
	}
	if req.ControllerID != "" && !provisionIDRe.MatchString(req.ControllerID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CONTROLLER_ID", "controller_id 형식 오류.", "")
		return
	}
	if req.Port != "" && !provisionPortRe.MatchString(req.Port) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PORT", "port 는 /dev/ttyUSB* 또는 /dev/ttyACM* 형식이어야 합니다.", "")
		return
	}
	if req.SiteID != "" && !provisionIDRe.MatchString(req.SiteID) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SITE_ID", "site_id 형식 오류.", "")
		return
	}

	// 동시 provisioning 차단 (secrets.h 공유).
	provisionJobs.mu.Lock()
	if provisionJobs.running {
		provisionJobs.mu.Unlock()
		writeError(w, http.StatusConflict, "PROVISION_BUSY", "다른 provisioning 이 진행 중입니다. 완료 후 다시 시도하세요.", "")
		return
	}
	cid := req.ControllerID
	if cid == "" {
		cid = "feeder_" + req.TankID
	}
	job := &provisionJob{
		ID:           common.NewID("prov"),
		Status:       "running",
		ControllerID: cid,
		TankID:       req.TankID,
		StartedAt:    common.FormatTime(common.NowUTC()),
	}
	provisionJobs.jobs[job.ID] = job
	provisionJobs.running = true
	provisionJobs.mu.Unlock()

	// args
	args := []string{"--yes", "--tank", req.TankID, "--type", req.Type}
	if req.ControllerID != "" {
		args = append(args, "--controller-id", req.ControllerID)
	}
	if req.Port != "" {
		args = append(args, "--port", req.Port)
	}
	if req.SiteID != "" {
		args = append(args, "--site", req.SiteID)
	}
	if req.Reclaim {
		args = append(args, "--reclaim")
	}
	if req.Activate {
		args = append(args, "--activate")
	}

	token := os.Getenv(s.cfg.API.Auth.OperatorTokenEnv)
	env := append(os.Environ(), "BLUEI_EDGE_OPERATOR_TOKEN="+token)

	go runProvisionJob(job, args, env)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":        job.ID,
		"controller_id": cid,
		"tank_id":       req.TankID,
		"status":        "running",
	})
}

// GET /v1/provision/{job_id} — job 상태/로그 스냅샷.
func (s *Server) handleProvisionItem(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/v1/provision/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "expected /v1/provision/{job_id}", "")
		return
	}
	provisionJobs.mu.Lock()
	job, ok := provisionJobs.jobs[id]
	var snap provisionJob
	if ok {
		snap = *job
	}
	provisionJobs.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "JOB_NOT_FOUND", "job 을 찾을 수 없습니다.", "")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// jobWriter — 자식 프로세스 출력(stdout+stderr)을 job.Log 에 동기적으로 누적.
type jobWriter struct{ job *provisionJob }

func (jw *jobWriter) Write(p []byte) (int, error) {
	provisionJobs.mu.Lock()
	jw.job.Log += string(p)
	provisionJobs.mu.Unlock()
	return len(p), nil
}

func runProvisionJob(job *provisionJob, args, env []string) {
	defer func() {
		provisionJobs.mu.Lock()
		provisionJobs.running = false
		job.FinishedAt = common.FormatTime(common.NowUTC())
		provisionJobs.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := provisionExec(ctx, args, env)
	jw := &jobWriter{job: job}
	cmd.Stdout = jw
	cmd.Stderr = jw

	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	var status string
	switch exit {
	case 0:
		status = "success"
	case 2:
		status = "flashed_unconfirmed"
	default:
		status = "failed"
	}
	provisionJobs.mu.Lock()
	job.ExitCode = exit
	job.Status = status
	provisionJobs.mu.Unlock()
}
