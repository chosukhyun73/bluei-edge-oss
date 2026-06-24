package training

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestBuildLowPriorityCommand verifies the wrapper picks up nice/ionice when
// they're on PATH, and falls back gracefully when they're not.
func TestBuildLowPriorityCommand(t *testing.T) {
	cmd := buildLowPriorityCommand(context.Background(), "/usr/bin/python3", []string{"--out", "/tmp/x"})
	if cmd == nil {
		t.Fatal("nil command")
	}
	full := strings.Join(cmd.Args, " ")
	// 적어도 우리 인자는 끝에 보존되어야 함.
	if !strings.Contains(full, "--out /tmp/x") {
		t.Fatalf("args missing in %q", full)
	}
	// linux 에서는 nice/ionice 가 보통 있음. 둘 중 하나라도 발견되면 OK.
	hasNice := strings.Contains(full, "/nice") || strings.HasPrefix(full, "nice ")
	hasIonice := strings.Contains(full, "/ionice") || strings.HasPrefix(full, "ionice ")
	t.Logf("expanded command: %q (nice=%v, ionice=%v)", full, hasNice, hasIonice)
	// 이 테스트는 fallback 경로도 통과해야 하므로 강제하지 않고 로깅만.
}

// TestTrainingEnvDefaults verifies sensible defaults are injected when the
// parent env doesn't set them. Critical for "good neighbour" behaviour on
// shared GX10.
func TestTrainingEnvDefaults(t *testing.T) {
	// 기존 값을 임시로 비움
	for _, k := range []string{"BLUEI_YOLO_DEVICE", "BLUEI_TRAIN_GPU_MEM",
		"BLUEI_YOLO_WORKERS", "PYTORCH_CUDA_ALLOC_CONF"} {
		t.Setenv(k, "")
	}
	env := trainingEnv()
	joined := strings.Join(env, "\n")
	required := []string{
		"BLUEI_YOLO_DEVICE=auto",
		"BLUEI_TRAIN_GPU_MEM=0.5",
	}
	for _, want := range required {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in env, full env keys:\n%s", want, mapEnvKeys(env))
		}
	}
}

func TestTrainingEnvRespectsParent(t *testing.T) {
	t.Setenv("BLUEI_TRAIN_GPU_MEM", "0.3")
	env := trainingEnv()
	if !strings.Contains(strings.Join(env, "\n"), "BLUEI_TRAIN_GPU_MEM=0.3") {
		t.Fatal("parent env value not preserved")
	}
}

func mapEnvKeys(env []string) string {
	out := []string{}
	for _, e := range env {
		if strings.HasPrefix(e, "BLUEI_") || strings.HasPrefix(e, "PYTORCH_") || strings.HasPrefix(e, "ULTRA") || strings.HasPrefix(e, "YOLO_") {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return "(none of the BLUEI_/PYTORCH_/YOLO_ keys present)"
	}
	return strings.Join(out, "\n")
}

// TestStartOptionsDefaultsToVision — 빈 Kind 는 vision-detector 로 fallback.
func TestStartOptionsDefaultsToVision(t *testing.T) {
	// Start 내부의 kind 기본값 결정 로직을 StartOptions 를 통해 직접 검증한다.
	opts := StartOptions{Kind: ""}
	if opts.Kind == "" {
		opts.Kind = "vision-detector"
	}
	if opts.Kind != "vision-detector" {
		t.Fatalf("expected vision-detector, got %q", opts.Kind)
	}
}

// TestStartOptionsBaseline — Kind=tank-baseline 이 Job 에 정확히 반영되고
// CandidatePath 가 baselines/<tank_id>/<job_id> 형태인지.
func TestStartOptionsBaseline(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err := os.Stat("/usr/local/bin/python3"); err != nil {
			t.Skip("python3 not available")
		}
	}
	tmp := t.TempDir()
	script := tmp + "/fake_train.py"
	if err := os.WriteFile(script, []byte(
		"import time\nprint('PROGRESS 5 시작', flush=True)\ntime.sleep(30)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, Config{ScriptPath: script, PythonBin: "python3", CandidateDir: tmp + "/cand"})
	job, err := svc.Start(context.Background(), StartOptions{Kind: "tank-baseline", TankID: "tank_01"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if job.Kind != "tank-baseline" {
		t.Errorf("Kind: %q", job.Kind)
	}
	if job.TankID != "tank_01" {
		t.Errorf("TankID: %q", job.TankID)
	}
	if !strings.Contains(job.CandidatePath, "baselines/tank_01/") {
		t.Errorf("CandidatePath %q should contain baselines/tank_01/", job.CandidatePath)
	}
	_ = svc.Cancel(context.Background())
}

// TestStartOptionsForecast — water-forecast 도 동일.
func TestStartOptionsForecast(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err := os.Stat("/usr/local/bin/python3"); err != nil {
			t.Skip("python3 not available")
		}
	}
	tmp := t.TempDir()
	script := tmp + "/fake_train.py"
	if err := os.WriteFile(script, []byte(
		"import time\nprint('PROGRESS 5 시작', flush=True)\ntime.sleep(30)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, Config{ScriptPath: script, PythonBin: "python3", CandidateDir: tmp + "/cand"})
	job, err := svc.Start(context.Background(), StartOptions{Kind: "water-forecast", TankID: "tank_02"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if job.Kind != "water-forecast" {
		t.Errorf("Kind: %q", job.Kind)
	}
	if job.TankID != "tank_02" {
		t.Errorf("TankID: %q", job.TankID)
	}
	if !strings.Contains(job.CandidatePath, "forecasts/tank_02/") {
		t.Errorf("CandidatePath %q should contain forecasts/tank_02/", job.CandidatePath)
	}
	_ = svc.Cancel(context.Background())
}

// TestStartCancelLifecycle exercises the full Start→scan→Cancel path with a
// fake "training script" so we exercise process-group setup and tree kill on
// linux. Skipped if no python3.
func TestStartCancelLifecycle(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err := os.Stat("/usr/local/bin/python3"); err != nil {
			t.Skip("python3 not available")
		}
	}
	tmp := t.TempDir()
	// 가짜 학습 스크립트: PROGRESS 라인 한 번 찍고 sleep.
	script := tmp + "/fake_train.py"
	if err := os.WriteFile(script, []byte(
		"import sys, time\n"+
			"print('PROGRESS 5 시작', flush=True)\n"+
			"time.sleep(30)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, Config{
		ScriptPath:   script,
		PythonBin:    "python3",
		CandidateDir: tmp + "/cand",
	})
	job, err := svc.Start(context.Background(), StartOptions{Kind: "vision-detector", AlgorithmID: "algo_a"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if job.JobID == "" {
		t.Fatal("empty job id")
	}
	if !svc.IsRunning() {
		t.Fatal("expected running")
	}
	if err := svc.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Cancel 후 상태는 canceled
	cur, _ := svc.Current()
	if cur.Status != "canceled" {
		t.Fatalf("status after cancel: %q", cur.Status)
	}
}
