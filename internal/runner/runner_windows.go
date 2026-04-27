//go:build windows

package runner

import (
	"context"
	"os/exec"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

func (r *Runner) buildCommand(ctx context.Context, rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) *exec.Cmd {
	cmd := exec.CommandContext(ctx, rt.Command, buildArgs(rt, model, prompt, s)...)
	return cmd
}
