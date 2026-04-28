//go:build !windows

package runner

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

func (r *Runner) buildCommand(ctx context.Context, rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) *exec.Cmd {
	cmd := exec.CommandContext(ctx, rt.Command, buildArgs(rt, model, prompt, s)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func (r *Runner) KillTask(taskID string) error {
	r.mu.Lock()
	cmd, ok := r.procs[taskID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %s not found or not running", taskID)
	}

	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)

	go func() {
		time.Sleep(3 * time.Second)
		r.mu.RLock()
		_, still := r.procs[taskID]
		r.mu.RUnlock()
		if still {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()

	return nil
}
