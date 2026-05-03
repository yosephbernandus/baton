package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	gitpkg "github.com/yosephbernandus/baton/internal/git"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/socket"
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
	SocketPath    string
	ErrorDetail   string
}

type Runner struct {
	cfg      *config.Config
	emitter  *events.Emitter
	store    *task.Store
	mu       sync.RWMutex
	procs    map[string]*exec.Cmd
}

func New(cfg *config.Config, emitter *events.Emitter, store *task.Store) *Runner {
	return &Runner{cfg: cfg, emitter: emitter, store: store, procs: make(map[string]*exec.Cmd)}
}

func (r *Runner) Run(ctx context.Context, taskID, runtimeName, model, prompt string, s *spec.Spec, timeout time.Duration) (*Result, error) {
	rt, ok := r.cfg.Runtimes[runtimeName]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found", runtimeName)
	}

	taskDir := fmt.Sprintf(".baton/tasks/%s", taskID)
	_ = os.MkdirAll(taskDir, 0o755)
	inboxPath := fmt.Sprintf("%s/inbox.ndjson", taskDir)
	if _, err := os.Stat(inboxPath); os.IsNotExist(err) {
		_ = os.WriteFile(inboxPath, nil, 0o644)
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

	r.mu.Lock()
	r.procs[taskID] = cmd
	r.mu.Unlock()

	if cmd.Process != nil {
		if t, err := r.store.Get(taskID); err == nil {
			t.PID = cmd.Process.Pid
			_ = r.store.Update(t)
		}
	}

	sockPath := fmt.Sprintf(".baton/tasks/%s/baton.sock", taskID)
	srv, srvErr := socket.NewServer(sockPath)
	if srvErr == nil {
		go srv.Accept(ctx)
		go r.handleIncoming(ctx, taskID, runtimeName, model, srv)
		defer srv.Close()
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

		if mk, ok := proto.ParseMarker(line); ok {
			r.emitMarkerEvent(taskID, runtimeName, model, mk)
			if srv != nil {
				_ = srv.Broadcast(proto.MarkerToMessage(mk))
			}
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

	status := r.determineStatus(exitCode, clarification, ctx, output)

	var errorDetail string
	if status == "failed" && exitCode == 0 {
		errorDetail = extractErrorDetail(output)
	}

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

	r.mu.Lock()
	delete(r.procs, taskID)
	r.mu.Unlock()

	var socketPathResult string
	if srvErr == nil {
		socketPathResult = sockPath
	}

	return &Result{
		Status:        status,
		ExitCode:      exitCode,
		Clarification: clarification,
		Output:        output,
		FilesChanged:  filesChanged,
		Duration:      duration,
		SocketPath:    socketPathResult,
		ErrorDetail:   errorDetail,
	}, nil
}


func buildArgs(rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) []string {
	var args []string

	for _, p := range rt.Positional {
		if p == "{{prompt}}" {
			args = append(args, prompt)
		} else {
			args = append(args, p)
		}
	}
	if rt.ModelFlag != "" {
		args = append(args, rt.ModelFlag, model)
	}
	if rt.ContextFlag != "" && s != nil && len(s.ContextFiles) > 0 {
		for _, f := range s.ContextFiles {
			args = append(args, rt.ContextFlag, f)
		}
	}
	args = append(args, rt.ExtraFlags...)
	if rt.PromptFlag != "" {
		args = append(args, rt.PromptFlag, prompt)
	}

	return args
}

func (r *Runner) determineStatus(exitCode int, clarification string, ctx context.Context, output []string) string {
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
	if detectOutputError(output) {
		return "failed"
	}
	return "completed"
}

var outputErrorPatterns = []string{
	"Error:",
	"error:",
	"Model not found",
	"model not found",
	"FATAL",
	"fatal error",
	"panic:",
	"command not found",
	"No such file or directory",
	"permission denied",
	"connection refused",
	"authentication failed",
}

func extractErrorDetail(output []string) string {
	tailStart := 0
	if len(output) > 10 {
		tailStart = len(output) - 10
	}
	for _, line := range output[tailStart:] {
		for _, pattern := range outputErrorPatterns {
			if strings.Contains(line, pattern) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

func detectOutputError(output []string) bool {
	tailStart := 0
	if len(output) > 20 {
		tailStart = len(output) - 20
	}
	for _, line := range output[tailStart:] {
		for _, pattern := range outputErrorPatterns {
			if strings.Contains(line, pattern) {
				return true
			}
		}
	}
	return false
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

func (r *Runner) handleIncoming(ctx context.Context, taskID, runtimeName, model string, srv *socket.Server) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-srv.Incoming():
			if !ok {
				return
			}
			switch msg.M {
			case "guide":
				inboxPath := fmt.Sprintf(".baton/tasks/%s/inbox.ndjson", taskID)
				guidance := fmt.Sprintf("{\"type\":\"guidance\",\"from\":%q,\"msg\":%q}\n", msg.From, msg.Msg)
				if f, err := os.OpenFile(inboxPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
					_, _ = f.WriteString(guidance)
					f.Close()
				}
				_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "guidance_received", map[string]interface{}{
					"from": msg.From,
					"msg":  msg.Msg,
				})
				_ = srv.Reply(msg, proto.Message{M: "ok", ID: msg.ID})
			case "abort":
				r.mu.RLock()
				cmd := r.procs[taskID]
				r.mu.RUnlock()
				if cmd != nil && cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = srv.Reply(msg, proto.Message{M: "ok", ID: msg.ID})
			}
		}
	}
}

func (r *Runner) emitMarkerEvent(taskID, runtimeName, model string, mk proto.Marker) {
	eventType := "worker_" + mk.Type.String()
	data := map[string]interface{}{
		"msg": mk.Msg,
	}
	if mk.Pct > 0 {
		data["pct"] = mk.Pct
	}
	_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", eventType, data)
}
