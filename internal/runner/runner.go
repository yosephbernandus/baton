package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	gitpkg "github.com/yosephbernandus/baton/internal/git"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/socket"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

// cancel reason constants for the watchdog goroutine
const (
	cancelNone int32 = iota
	cancelSilenceTimeout
	cancelAbsoluteTimeout
)

// LivenessConfig controls how the runner detects and handles unresponsive workers.
type LivenessConfig struct {
	SilenceTimeout     time.Duration
	AbsoluteTimeout    time.Duration
	SilenceWarning     time.Duration
	StartupTimeout     time.Duration
	NetworkIdleTimeout time.Duration
	AttemptTimeout     time.Duration
	TickInterval       time.Duration // how often watchdog checks; 0 defaults to 30s
}

type Result struct {
	Status        string
	ExitCode      int
	Crashed       bool
	Clarification string
	Output        []string
	ChecksFailed  []string
	FilesChanged  []string
	Duration      time.Duration
	SocketPath    string
	ErrorDetail   string
}

type Runner struct {
	cfg     *config.Config
	emitter *events.Emitter
	store   *task.Store
	mu      sync.RWMutex
	procs   map[string]*exec.Cmd
}

func New(cfg *config.Config, emitter *events.Emitter, store *task.Store) *Runner {
	return &Runner{cfg: cfg, emitter: emitter, store: store, procs: make(map[string]*exec.Cmd)}
}

func (r *Runner) KillAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cmd := range r.procs {
		killProcessGroup(cmd)
	}
}

func (r *Runner) Run(ctx context.Context, taskID, runtimeName, model, prompt string, s *spec.Spec, liveness LivenessConfig, extraArgs ...string) (*Result, error) {
	rt, ok := r.cfg.Runtimes[runtimeName]
	if !ok {
		return nil, fmt.Errorf("runtime %q not found", runtimeName)
	}

	// Create an internal cancellable context; the caller's ctx is still
	// respected for external cancellation (e.g. kill command).
	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	taskDir := fmt.Sprintf(".baton/tasks/%s", taskID)
	_ = os.MkdirAll(taskDir, 0o755)
	inboxPath := fmt.Sprintf("%s/inbox.ndjson", taskDir)
	if _, err := os.Stat(inboxPath); os.IsNotExist(err) {
		_ = os.WriteFile(inboxPath, nil, 0o644)
	}

	cmd := r.buildCommand(innerCtx, &rt, model, prompt, s, extraArgs...)

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

	// Kill entire process group on cancel so orphaned children release the pipe.
	go func() {
		<-innerCtx.Done()
		killProcessGroup(cmd)
	}()

	if cmd.Process != nil {
		if t, err := r.store.Get(taskID); err == nil {
			t.PID = cmd.Process.Pid
			now := time.Now().UTC()
			t.LastActivity = &now
			_ = r.store.Update(t)
		}
		_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_pid", map[string]interface{}{
			"pid": cmd.Process.Pid,
		})
	}

	sockPath := fmt.Sprintf(".baton/tasks/%s/baton.sock", taskID)
	srv, srvErr := socket.NewServer(sockPath)
	if srvErr == nil {
		go srv.Accept(innerCtx)
		go r.handleIncoming(innerCtx, taskID, runtimeName, model, srv)
		defer srv.Close()
	}

	// Liveness tracking atomics
	var lastActivity atomic.Int64
	var protocolAware atomic.Bool
	var isStuck atomic.Bool
	var cancelReason atomic.Int32

	lastActivity.Store(time.Now().UnixMilli())

	// Watchdog goroutine: 5-level timeout hierarchy.
	// StartupTimeout → NetworkIdleTimeout → HeartbeatTimeout(SilenceTimeout) → AttemptTimeout → AbsoluteTimeout
	go func() {
		tickInterval := liveness.TickInterval
		if tickInterval <= 0 {
			tickInterval = 30 * time.Second
		}
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()

		absoluteTimeout := liveness.AbsoluteTimeout
		if absoluteTimeout <= 0 {
			absoluteTimeout = 2 * time.Hour
		}
		absoluteTimer := time.NewTimer(absoluteTimeout)
		defer absoluteTimer.Stop()

		attemptTimeout := liveness.AttemptTimeout
		var attemptTimer *time.Timer
		if attemptTimeout > 0 {
			attemptTimer = time.NewTimer(attemptTimeout)
			defer attemptTimer.Stop()
		}

		startupTimeout := liveness.StartupTimeout
		if startupTimeout <= 0 {
			startupTimeout = 2 * time.Minute
		}
		networkIdleTimeout := liveness.NetworkIdleTimeout
		if networkIdleTimeout <= 0 {
			networkIdleTimeout = 2 * time.Minute
		}

		warned := false
		startupDone := false
		pid := 0
		if cmd.Process != nil {
			pid = cmd.Process.Pid
		}

		for {
			var attemptCh <-chan time.Time
			if attemptTimer != nil {
				attemptCh = attemptTimer.C
			}

			select {
			case <-innerCtx.Done():
				return
			case <-absoluteTimer.C:
				_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_timeout", map[string]interface{}{
					"reason": "absolute_timeout",
				})
				cancelReason.Store(cancelAbsoluteTimeout)
				cancel()
				return
			case <-attemptCh:
				_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_timeout", map[string]interface{}{
					"reason": "attempt_timeout",
				})
				cancelReason.Store(cancelAbsoluteTimeout)
				cancel()
				return
			case <-ticker.C:
				last := time.UnixMilli(lastActivity.Load())
				silence := time.Since(last)

				if !startupDone {
					if lastActivity.Load() > time.Now().Add(-1*time.Second).UnixMilli() {
						startupDone = true
					} else if silence >= startupTimeout {
						_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_timeout", map[string]interface{}{
							"reason": "startup_timeout",
						})
						cancelReason.Store(cancelSilenceTimeout)
						cancel()
						return
					}
					continue
				}

				if isStuck.Load() {
					continue
				}

				if pid > 0 && checkNetworkActivity(pid) {
					if warned {
						warned = false
					}
					continue
				}

				if !protocolAware.Load() {
					if silence >= networkIdleTimeout {
						_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_killed", map[string]interface{}{
							"reason":  "network_idle_timeout",
							"silence": silence.Round(time.Second).String(),
						})
						cancelReason.Store(cancelSilenceTimeout)
						cancel()
						return
					}
					continue
				}

				if silence >= liveness.SilenceTimeout {
					_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_killed", map[string]interface{}{
						"reason":  "silence_timeout",
						"silence": silence.Round(time.Second).String(),
					})
					cancelReason.Store(cancelSilenceTimeout)
					cancel()
					return
				}
				if !warned && silence >= liveness.SilenceWarning {
					warned = true
					_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "worker_unresponsive", map[string]interface{}{
						"silence": silence.Round(time.Second).String(),
					})
				}
				if warned && silence < liveness.SilenceWarning {
					warned = false
				}
			}
		}
	}()

	var output []string
	var clarification string
	var lastStoreTouchMs int64
	isStreamJSON := rt.OutputFormat == "stream-json"

	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		nowMs := time.Now().UnixMilli()
		lastActivity.Store(nowMs)
		isStuck.Store(false)

		if nowMs-lastStoreTouchMs > 30_000 {
			lastStoreTouchMs = nowMs
			_ = r.store.TouchActivity(taskID)
		}

		if isStreamJSON {
			displayLine, rText, isResult, sjOK := parseStreamJSON(line)
			if sjOK {
				output = append(output, line)
				if isResult && rText != "" {
					output = append(output, rText)
				}
				if displayLine != "" {
					_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "output", map[string]interface{}{
						"stream": "stdout",
						"line":   displayLine,
					})
					for _, dl := range strings.Split(displayLine, "\n") {
						if cl := extractClarification(dl); cl != "" {
							clarification = cl
						}
						if mk, ok := proto.ParseMarker(dl); ok {
							protocolAware.Store(true)
							if mk.Type == proto.MarkerStuck {
								isStuck.Store(true)
							}
							r.emitMarkerEvent(taskID, runtimeName, model, mk)
							if srv != nil {
								_ = srv.Broadcast(proto.MarkerToMessage(mk))
							}
						}
					}
				}
				if isResult && rText != "" {
					for _, rl := range strings.Split(rText, "\n") {
						if mk, ok := proto.ParseMarker(rl); ok {
							protocolAware.Store(true)
							r.emitMarkerEvent(taskID, runtimeName, model, mk)
						}
					}
				}
				continue
			}
		}

		output = append(output, line)

		_ = r.emitter.TaskEvent(taskID, runtimeName, model, "", "output", map[string]interface{}{
			"stream": "stdout",
			"line":   line,
		})

		if cl := extractClarification(line); cl != "" {
			clarification = cl
		}

		if mk, ok := proto.ParseMarker(line); ok {
			protocolAware.Store(true)
			if mk.Type == proto.MarkerStuck {
				isStuck.Store(true)
			}
			r.emitMarkerEvent(taskID, runtimeName, model, mk)
			if srv != nil {
				_ = srv.Broadcast(proto.MarkerToMessage(mk))
			}
		}
	}

	err = cmd.Wait()
	duration := time.Since(start)
	exitCode := 0
	crashed := false
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			if exitCode == 137 || exitCode == -1 {
				crashed = true
			}
		} else {
			crashed = true
			exitCode = -1
		}
	}

	status := r.determineStatusForRuntime(exitCode, clarification, cancelReason.Load(), output, runtimeName, protocolAware.Load())

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
		Crashed:       crashed,
		Clarification: clarification,
		Output:        output,
		FilesChanged:  filesChanged,
		Duration:      duration,
		SocketPath:    socketPathResult,
		ErrorDetail:   errorDetail,
	}, nil
}

func BuildArgs(rt *config.RuntimeConfig, model, prompt string, s *spec.Spec) []string {
	var args []string
	stdinMode := rt.PromptMode == "stdin"

	for _, p := range rt.Positional {
		if p == "{{prompt}}" {
			if !stdinMode {
				args = append(args, prompt)
			}
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
	if rt.PromptFlag != "" && !stdinMode {
		args = append(args, rt.PromptFlag, prompt)
	}

	return args
}

func BuildToolRestrictionFlags(rt *config.RuntimeConfig, allowedTools []string) []string {
	if rt.ToolRestriction == nil || rt.ToolRestriction.Flag == "" || len(allowedTools) == 0 {
		return nil
	}
	switch rt.ToolRestriction.Format {
	case "comma-separated":
		return []string{rt.ToolRestriction.Flag, strings.Join(allowedTools, ",")}
	case "repeat":
		var flags []string
		for _, tool := range allowedTools {
			flags = append(flags, rt.ToolRestriction.Flag, tool)
		}
		return flags
	default:
		return []string{rt.ToolRestriction.Flag, strings.Join(allowedTools, ",")}
	}
}

func (r *Runner) determineStatusForRuntime(exitCode int, clarification string, reason int32, output []string, runtimeName string, protocolAware bool) string {
	if reason == cancelSilenceTimeout || reason == cancelAbsoluteTimeout {
		return "timeout"
	}
	if exitCode == r.cfg.ClarifyExit {
		return "needs_clarification"
	}
	if exitCode != 0 && clarification != "" {
		for _, pattern := range r.cfg.ClarifyPatterns {
			if strings.Contains(clarification, pattern) {
				return "needs_clarification"
			}
		}
	}
	if r.detectRateLimit(exitCode, output, runtimeName) {
		return "rate_limited"
	}
	if exitCode != 0 {
		return "failed"
	}
	if !protocolAware && detectOutputError(output) {
		return "failed"
	}
	return "completed"
}

var defaultRateLimitPatterns = []string{
	"rate limit", "rate_limit", "429",
	"too many requests", "quota exceeded",
	"overloaded", "capacity", "token limit",
	"retry after", "retry-after",
}

func (r *Runner) detectRateLimit(exitCode int, output []string, runtimeName string) bool {
	var patterns []string
	var exitCodes []int

	if rt, ok := r.cfg.Runtimes[runtimeName]; ok && rt.RateLimit != nil {
		patterns = rt.RateLimit.Patterns
		exitCodes = rt.RateLimit.ExitCodes
	}
	if len(patterns) == 0 {
		patterns = defaultRateLimitPatterns
	}

	for _, code := range exitCodes {
		if exitCode == code {
			return true
		}
	}

	tailStart := 0
	if len(output) > 5 {
		tailStart = len(output) - 5
	}
	for _, line := range output[tailStart:] {
		lower := strings.ToLower(line)
		for _, pattern := range patterns {
			if strings.Contains(lower, strings.ToLower(pattern)) {
				return true
			}
		}
	}

	return false
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
				killProcessGroup(cmd)
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
