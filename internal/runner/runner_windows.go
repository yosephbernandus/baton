//go:build windows

package runner

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

func (r *Runner) buildCommand(ctx context.Context, rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) *exec.Cmd {
	cmd := exec.CommandContext(ctx, rt.Command, buildArgs(rt, model, prompt, s)...)
	return cmd
}

func (r *Runner) KillTask(taskID string) error {
	r.mu.Lock()
	cmd, ok := r.procs[taskID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %s not found or not running", taskID)
	}

	if err := cmd.Process.Kill(); err != nil {
		return fmt.Errorf("killing task %s: %w", taskID, err)
	}

	return nil
}
