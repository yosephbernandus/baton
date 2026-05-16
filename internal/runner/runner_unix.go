//go:build !windows

package runner

import (
	"context"
	"os/exec"
	"strings"
	"syscall"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

func (r *Runner) buildCommand(_ context.Context, rt *config.RuntimeConfig, model, prompt string, s *spec.Spec, extraArgs ...string) *exec.Cmd {
	args := BuildArgs(rt, model, prompt, s)
	args = append(args, extraArgs...)
	cmd := exec.Command(rt.Command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if rt.PromptMode == "stdin" {
		cmd.Stdin = strings.NewReader(prompt)
	}
	return cmd
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
