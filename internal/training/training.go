// Package training manages user-triggered AI training jobs as background
// subprocesses. It is designed for non-developer operators interacting via the
// local UI:
//
//   - At most one job runs concurrently (mutex).
//   - Progress is appended to the SQLite event log so the UI can poll.
//   - Job results land in candidate_path. Promotion to the live runtime is a
//     separate, explicit operator action (handled by the API layer).
//   - The rules engine remains the safety owner. Training never alters
//     real-time control logic on its own.
package training

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
)

// Service runs and tracks a single training job at a time.
type Service struct {
	app          *runtime.App
	scriptPath   string // 절대 경로 또는 작업 디렉터리 기준 상대 경로
	pythonBin    string // 기본: "python3"
	candidateDir string // 학습 결과 weights 디렉터리 (자동 생성)
	timeout      time.Duration

	mu      sync.Mutex
	current *Job // nil 이면 idle
}

// Job represents a running or recently completed training job.
type Job struct {
	JobID         string
	Kind          string // vision-detector | tank-baseline | water-forecast
	AlgorithmID   string // vision-detector 일 때 의미
	TankID        string // tank-baseline / water-forecast 일 때 의미
	Status        string // started | progress | completed | failed | canceled
	StageLabel    string
	ProgressPct   float64
	CandidatePath string
	Metrics       map[string]any
	Error         string
	StartedAt     time.Time
	UpdatedAt     time.Time

	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// StartOptions specifies what kind of training job to launch.
type StartOptions struct {
	Kind        string // 필수. "" 면 vision-detector 로 기본
	AlgorithmID string // vision-detector 시 필수
	TankID      string // tank-baseline / water-forecast 시 필수
	DatasetPath string // 옵션 (vision 학습 시)
}

// Snapshot returns a copy safe to hand out via API/UI.
func (j *Job) Snapshot() Job {
	if j == nil {
		return Job{}
	}
	out := *j
	out.cmd = nil
	out.cancel = nil
	return out
}

// Config selects the script and where to put weights.
type Config struct {
	ScriptPath   string
	PythonBin    string
	CandidateDir string
	Timeout      time.Duration
}

// New constructs a Service. ScriptPath/PythonBin/CandidateDir/Timeout default
// to safe values when unset.
func New(app *runtime.App, cfg Config) *Service {
	if cfg.ScriptPath == "" {
		// run_training.py 가 export_labels → train_detector 를 직렬로 호출하고
		// PROGRESS / METRIC 라인을 표준출력으로 내보낸다. UI 진행률은 이 라인에서 계산.
		cfg.ScriptPath = "local-ai/training/run_training.py"
	}
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}
	if cfg.CandidateDir == "" {
		cfg.CandidateDir = "local-ai/training/runs/candidates"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Minute
	}
	return &Service{
		app:          app,
		scriptPath:   cfg.ScriptPath,
		pythonBin:    cfg.PythonBin,
		candidateDir: cfg.CandidateDir,
		timeout:      cfg.Timeout,
	}
}

// Current returns a snapshot of the current/last job. ok=false when never run.
func (s *Service) Current() (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return Job{}, false
	}
	return s.current.Snapshot(), true
}

// IsRunning reports whether a job is in started/progress state.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return false
	}
	switch s.current.Status {
	case "started", "progress":
		return true
	}
	return false
}

// Start launches a new training job. Returns ErrBusy if another job is running.
//
// Resource isolation:
//   - 자식 프로세스는 별도 process group 으로 격리되어, 학습 freeze/OOM 시
//     Cancel 호출이 트리 전체를 깔끔히 종료할 수 있다.
//   - nice/ionice 우선순위를 낮춰 Go 런타임/카메라 ingest/ESP32 polling 이
//     CPU/IO 경합에서 굶지 않도록 한다.
//   - GPU 메모리는 자식이 PyTorch fraction 으로 cap (run_training.py).
//   - PYTORCH_CUDA_ALLOC_CONF 단편화 완화로 long-running 안정성 확보.
func (s *Service) Start(ctx context.Context, opts StartOptions) (Job, error) {
	if opts.Kind == "" {
		opts.Kind = "vision-detector"
	}

	s.mu.Lock()
	if s.current != nil && (s.current.Status == "started" || s.current.Status == "progress") {
		s.mu.Unlock()
		return Job{}, ErrBusy
	}

	jobID := common.NewID("train_job")

	// Kind 별 candidatePath 결정
	var candidatePath string
	switch opts.Kind {
	case "tank-baseline":
		candidatePath = filepath.Join("local-ai/training/runs/baselines", opts.TankID, jobID)
	case "water-forecast":
		candidatePath = filepath.Join("local-ai/training/runs/forecasts", opts.TankID, jobID)
	case "lrcn-finetune":
		// R5: LRCN fc2 fine-tune. 알고리즘별 runs 디렉토리.
		candidatePath = filepath.Join("local-ai/training/runs/lrcn", opts.AlgorithmID, jobID)
	default:
		candidatePath = filepath.Join(s.candidateDir, jobID)
	}

	jobCtx, cancel := context.WithTimeout(context.Background(), s.timeout)

	// nice/ionice 가 있는 환경에서는 우선순위를 낮춘 wrapper 로 호출.
	// 둘 다 없으면 그냥 python 직접 실행 (Docker slim 이미지 등).
	pyArgs := []string{s.scriptPath,
		"--kind", opts.Kind,
		"--tank-id", opts.TankID,
		"--dataset", opts.DatasetPath,
		"--out", candidatePath,
		"--algorithm-id", opts.AlgorithmID}
	cmd := buildLowPriorityCommand(jobCtx, s.pythonBin, pyArgs)
	cmd.Env = trainingEnv()
	cmd.SysProcAttr = lowPrioritySysProcAttr()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		s.mu.Unlock()
		return Job{}, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // Python tqdm/print 모두 한 스트림으로

	job := &Job{
		JobID:         jobID,
		Kind:          opts.Kind,
		AlgorithmID:   opts.AlgorithmID,
		TankID:        opts.TankID,
		Status:        "started",
		StageLabel:    "학습 준비 중",
		CandidatePath: candidatePath,
		StartedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
		cmd:           cmd,
		cancel:        cancel,
	}
	s.current = job
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		s.markFailed(job, fmt.Sprintf("프로세스 시작 실패: %v", err))
		return job.Snapshot(), err
	}
	s.appendEvent(ctx, job)

	go s.scan(stdout, job)
	go s.wait(ctx, job)

	return job.Snapshot(), nil
}

// Cancel terminates the current job. No-op if idle.
//
// Kills the entire process group (set up via Setpgid in Start) so that
// Ultralytics dataloader worker processes are also reaped. Otherwise they
// would outlive the parent and continue holding GPU memory.
func (s *Service) Cancel(ctx context.Context) error {
	s.mu.Lock()
	job := s.current
	s.mu.Unlock()
	if job == nil || (job.Status != "started" && job.Status != "progress") {
		return ErrNotRunning
	}
	if job.cmd != nil && job.cmd.Process != nil {
		killProcessTree(job.cmd.Process.Pid)
	}
	if job.cancel != nil {
		job.cancel()
	}
	s.mu.Lock()
	job.Status = "canceled"
	job.StageLabel = "운영자 취소"
	job.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
	s.appendEvent(ctx, job)
	return nil
}

// scan parses subprocess output for progress hints.
// Convention (training scripts may print one of these per line):
//
//	PROGRESS <pct> <stage_korean>
//	METRIC <key> <value>
//	COMPLETED <metrics_json_optional>
//
// Anything else is ignored (Python tqdm/log noise).
func (s *Service) scan(r io.Reader, job *Job) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "PROGRESS ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "PROGRESS "), " ", 2)
			if len(parts) >= 1 {
				if pct, err := strconv.ParseFloat(parts[0], 64); err == nil {
					s.mu.Lock()
					job.Status = "progress"
					job.ProgressPct = pct
					if len(parts) == 2 {
						job.StageLabel = parts[1]
					}
					job.UpdatedAt = time.Now().UTC()
					s.mu.Unlock()
					s.appendEvent(context.Background(), job)
				}
			}
		} else if strings.HasPrefix(line, "METRIC ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "METRIC "), " ", 2)
			if len(parts) == 2 {
				s.mu.Lock()
				if job.Metrics == nil {
					job.Metrics = map[string]any{}
				}
				if v, err := strconv.ParseFloat(parts[1], 64); err == nil {
					job.Metrics[parts[0]] = v
				} else {
					job.Metrics[parts[0]] = parts[1]
				}
				job.UpdatedAt = time.Now().UTC()
				s.mu.Unlock()
			}
		}
	}
}

// wait blocks on the subprocess and finalizes job state.
func (s *Service) wait(ctx context.Context, job *Job) {
	err := job.cmd.Wait()
	if job.cancel != nil {
		job.cancel()
	}
	s.mu.Lock()
	if job.Status == "canceled" {
		s.mu.Unlock()
		return
	}
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		job.StageLabel = "학습 실패"
	} else {
		job.Status = "completed"
		job.StageLabel = "학습 완료"
		pct := 100.0
		job.ProgressPct = pct
	}
	job.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
	s.appendEvent(ctx, job)
}

func (s *Service) markFailed(job *Job, msg string) {
	s.mu.Lock()
	job.Status = "failed"
	job.Error = msg
	job.StageLabel = "학습 실패"
	job.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
	s.appendEvent(context.Background(), job)
}

func (s *Service) appendEvent(ctx context.Context, job *Job) {
	if s.app == nil {
		return
	}
	// kind / tank_id 를 Metrics 에 편승 — 이벤트 스키마 변경 없이 UI/sync 에서 조회 가능.
	metrics := job.Metrics
	if job.Kind != "" || job.TankID != "" {
		merged := map[string]any{}
		for k, v := range metrics {
			merged[k] = v
		}
		if job.Kind != "" {
			merged["kind"] = job.Kind
		}
		if job.TankID != "" {
			merged["tank_id"] = job.TankID
		}
		metrics = merged
	}
	pct := job.ProgressPct
	// AppendEvent subject: non-vision 은 tank_id, vision 은 algorithm_id
	subjectID := job.AlgorithmID
	if job.Kind == "tank-baseline" || job.Kind == "water-forecast" {
		subjectID = job.TankID
	}
	payload := events.VisionTrainingJobUpdatePayload{
		JobID:         job.JobID,
		AlgorithmID:   job.AlgorithmID,
		Status:        job.Status,
		StageLabel:    job.StageLabel,
		ProgressPct:   &pct,
		CandidatePath: job.CandidatePath,
		Metrics:       metrics,
		Error:         job.Error,
		UpdatedAt:     common.FormatTime(job.UpdatedAt),
	}
	_, _ = s.app.AppendEvent(ctx, "training", "subprocess", subjectID,
		events.EventVisionTrainingJobUpdate, job.JobID, payload)
}

// ErrBusy is returned when a job is already running.
var ErrBusy = errors.New("training: another job is already running")

// ErrNotRunning is returned when Cancel is called with no active job.
var ErrNotRunning = errors.New("training: no active job to cancel")
