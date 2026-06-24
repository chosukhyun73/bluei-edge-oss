//go:build !linux

package training

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// Non-linux fallback: no nice/ionice, no PGID isolation. Used for unit tests
// on macOS and CI containers; production GX10 deployment is linux.

func buildLowPriorityCommand(ctx context.Context, pythonBin string, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, pythonBin, args...)
}

func lowPrioritySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func trainingEnv() []string {
	env := os.Environ()
	if os.Getenv("BLUEI_YOLO_DEVICE") == "" {
		env = append(env, "BLUEI_YOLO_DEVICE=auto")
	}
	if os.Getenv("BLUEI_TRAIN_GPU_MEM") == "" {
		env = append(env, "BLUEI_TRAIN_GPU_MEM=0.5")
	}
	return env
}

// killProcessTree on non-linux: just kill the single process.
func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}
