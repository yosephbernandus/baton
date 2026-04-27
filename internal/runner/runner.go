package runner

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	gitpkg "github.com/yosephbernandus/baton/internal/git"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

type Result struct {
	Status        string
	ExitCode      int
	Clarification string
	Output        []string
	ChecksFailed  []string
	FilesChanged  []string
	Duration      time.Duration
}

type Runner struct {
	cfg     *config.Config
	emitter *events.Emitter
	store   *task.Store
}

func New(cfg *config.Config, emitter *events.Emitter, store *task.Store) *Runner {
	return &Runner{cfg: cfg, emitter: emitter, store: store}
}

func (r *Runner) Run(ctx context.Context, taskID, runtimeName, model, prompt string, s *spec.Spec, timeout time.Duration) (*Result, error) {
	rt, ok := r.cfg.Runtimes[runtimeName]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found", runtimeName)
	}

	cmd := r.buildCommand(ctx, &rt, model, prompt, s)

	_ = r.emitter.TaskEvent(taskID, runtimeName, model, r.cfg.Orchestrator.Runtime+"/"+r.cfg.Orchestrator.Model, "task_started", map[string]interface{}{
		"attempt": 1,
	})

	beforeSnap, _ := gitpkg.TakeSnapshot()

	start := time.Now()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting worker: %w", err)
	}

	var output []string
	var clarification string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		output = append(output, line)

		_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "output", map[string]interface{}{
			"stream": "stdout",
			"line":   line,
		})

		if cl := extractClarification(line); cl != "" {
			clarification = cl
		}
	}

	err = cmd.Wait()
	duration := time.Since(start)
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("waiting for worker: %w", err)
		}
	}

	status := r.determineStatus(exitCode, clarification, ctx)

	if status == "completed" && s != nil && len(s.AcceptanceChecks) > 0 {
		failed := r.runAcceptanceChecks(taskID, runtimeName, model, s.AcceptanceChecks)
		if len(failed) > 0 {
			status = "failed"
			return &Result{
				Status:       status,
				ExitCode:     exitCode,
				Output:       output,
				ChecksFailed: failed,
				Duration:     duration,
			}, nil
		}
	}

	var filesChanged []string
	afterSnap, _ := gitpkg.TakeSnapshot()
	if beforeSnap != nil && afterSnap != nil {
		filesChanged = gitpkg.DetectChanges(beforeSnap, afterSnap)
		for _, f := range filesChanged {
			_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "file_changed", map[string]interface{}{
				"path": f,
			})
		}
	}

	eventType := "task_" + status
	if status == "needs_clarification" {
		eventType = "needs_clarification"
	}
	_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", eventType, map[string]interface{}{
		"exit_code": exitCode,
		"duration":  duration.Round(time.Second).String(),
	})

	return &Result{
		Status:        status,
		ExitCode:      exitCode,
		Clarification: clarification,
		Output:        output,
		FilesChanged:  filesChanged,
		Duration:      duration,
	}, nil
}

func buildArgs(rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) []string {
	var args []string

	if rt.ModelFlag != "" {
		args = append(args, rt.ModelFlag, model)
	}
	if rt.ContextFlag != "" && s != nil && len(s.ContextFiles) > 0 {
		args = append(args, rt.ContextFlag, strings.Join(s.ContextFiles, ","))
	}
	args = append(args, rt.ExtraFlags...)
	if rt.PromptFlag != "" {
		args = append(args, rt.PromptFlag, prompt)
	}
	for _, p := range rt.Positional {
		if p == "{{prompt}}" {
			args = append(args, prompt)
		} else {
			args = append(args, p)
		}
	}

	return args
}

func (r *Runner) determineStatus(exitCode int, clarification string, ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout"
	}
	if exitCode == r.cfg.ClarifyExit {
		return "needs_clarification"
	}
	if exitCode != 0 && clarification != "" {
		for _, pattern := range r.cfg.ClarifyPatterns {
			if clarification != "" {
				return "needs_clarification"
			}
			_ = pattern
		}
	}
	if exitCode != 0 {
		return "failed"
	}
	return "completed"
}

func (r *Runner) runAcceptanceChecks(taskID, runtimeName, model string, checks []spec.Check) []string {
	var failed []string
	for _, check := range checks {
		cmd := exec.Command("sh", "-c", check.Command)
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		if exitCode == check.ExpectExit {
			_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "acceptance_check_passed", map[string]interface{}{
				"command":     check.Command,
				"description": check.Description,
			})
		} else {
			_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "acceptance_check_failed", map[string]interface{}{
				"command":     check.Command,
				"description": check.Description,
				"expected":    check.ExpectExit,
				"got":         exitCode,
			})
			failed = append(failed, check.Description)
		}
	}
	return failed
}

func extractClarification(line string) string {
	prefix := "CLARIFICATION_NEEDED:"
	if idx := strings.Index(line, prefix); idx >= 0 {
		return strings.TrimSpace(line[idx+len(prefix):])
	}
	return ""
}
