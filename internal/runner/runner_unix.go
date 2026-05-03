//go:build !windows

package runner

import (
	"context"
	"os/exec"
	"syscall"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

func (r *Runner) buildCommand(_ context.Context, rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) *exec.Cmd {
	// Use exec.Command (not CommandContext) — cancellation is handled by the
	// killProcessGroup goroutine in Run, which kills the entire process group
	// so orphaned children (e.g. sleep spawned by bash) don't hold the stdout
	// pipe open and block the scanner.
	cmd := exec.Command(rt.Command, buildArgs(rt, model, prompt, s)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
