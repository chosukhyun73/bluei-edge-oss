//go:build linux

package training

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// buildLowPriorityCommand wraps the python invocation with nice/ionice when
// they are available on PATH so the training subprocess is preempted by
// vision ingest, ESP32 polling, and the rules engine under contention.
//
// Order of preference:
//
//	ionice -c 3 nice -n 10 python ...   (best — both IO idle and CPU low)
//	nice -n 10 python ...               (CPU only)
//	python ...                          (no wrappers found)
func buildLowPriorityCommand(ctx context.Context, pythonBin string, args []string) *exec.Cmd {
	full := append([]string{}, args...)
	if niceBin, err := exec.LookPath("nice"); err == nil {
		// "-n 10" lowers CPU priority but does not require sudo.
		full = append([]string{niceBin, "-n", "10", pythonBin}, full...)
	} else {
		full = append([]string{pythonBin}, full...)
	}
	if ioniceBin, err := exec.LookPath("ionice"); err == nil {
		// "-c 3" puts the process and its children into the IO idle class.
		full = append([]string{ioniceBin, "-c", "3"}, full...)
	}
	return exec.CommandContext(ctx, full[0], full[1:]...)
}

// lowPrioritySysProcAttr places the training process in its own process group
// so Cancel can kill the entire subtree (ultralytics spawns dataloader
// workers that would otherwise outlive the parent).
func lowPrioritySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// trainingEnv builds the env block for the training subprocess. It inherits
// the parent env but overrides a few keys so PyTorch + ultralytics behave
// like a good neighbour to the rest of the runtime.
func trainingEnv() []string {
	env := os.Environ()
	overrides := map[string]string{
		// Default to GPU auto-detect; user can force cpu via parent env.
		"BLUEI_YOLO_DEVICE": getOrDefault("BLUEI_YOLO_DEVICE", "auto"),
		// 50% GPU memory budget. Vision/vllm/ollama get the rest.
		"BLUEI_TRAIN_GPU_MEM": getOrDefault("BLUEI_TRAIN_GPU_MEM", "0.5"),
		// Reduce dataloader workers — GX10 has limited cores when other
		// services are running.
		"BLUEI_YOLO_WORKERS": getOrDefault("BLUEI_YOLO_WORKERS", "2"),
		// Mitigate fragmentation on long-running runs.
		"PYTORCH_CUDA_ALLOC_CONF": getOrDefault("PYTORCH_CUDA_ALLOC_CONF", "max_split_size_mb:256"),
		// Quiet Ultralytics auto-update / telemetry.
		"YOLO_VERBOSE":          "False",
		"ULTRALYTICS_NO_BANNER": "1",
	}
	// Apply overrides only if not already present.
	for k, v := range overrides {
		if os.Getenv(k) == "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func getOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// killProcessTree sends SIGTERM to the negative pid (== process group), then
// after a 5s grace period SIGKILL. The negative-pid trick works because the
// child was started with Setpgid=true.
func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		for i := 0; i < 5; i++ {
			if err := syscall.Kill(-pid, 0); err != nil {
				return
			}
			time.Sleep(time.Second)
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}()
}
