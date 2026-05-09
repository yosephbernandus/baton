package advisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Request struct {
	Spec         string   `yaml:"spec"`
	Scratchpad   string   `yaml:"scratchpad"`
	OutputTails  []string `yaml:"output_tails"`
	Phase        int      `yaml:"phase"`
	PhaseName    string   `yaml:"phase_name"`
	Role         string   `yaml:"role"`
	L1Attempts   int      `yaml:"l1_attempts"`
	L2Cycles     int      `yaml:"l2_cycles"`
	LoopDetected bool     `yaml:"loop_detected"`
	FilesChanged []string `yaml:"files_changed"`
}

type Response struct {
	Action     string `yaml:"action"`
	Detail     string `yaml:"detail"`
	Confidence string `yaml:"confidence"`
}

const (
	ActionRetryWithHint    = "retry_with_hint"
	ActionSkipPhase        = "skip_phase"
	ActionEscalateToHuman  = "escalate_to_human"
	ActionModifyConstraints = "modify_constraints"
)

type Config struct {
	Enabled            bool   `yaml:"enabled"`
	Runtime            string `yaml:"runtime"`
	Model              string `yaml:"model"`
	MaxCallsPerSession int    `yaml:"max_calls_per_session"`
	MaxCallsPerTask    int    `yaml:"max_calls_per_task"`
	Timeout            string `yaml:"timeout"`
}

type Runner interface {
	Run(ctx context.Context, taskID, runtimeName, model, prompt string) ([]string, error)
}

type Advisor struct {
	cfg          Config
	runner       Runner
	taskDir      string
	sessionCalls int
	taskCalls    map[string]int
}

func New(cfg Config, runner Runner, taskDir string) *Advisor {
	return &Advisor{
		cfg:       cfg,
		runner:    runner,
		taskDir:   taskDir,
		taskCalls: make(map[string]int),
	}
}

func (a *Advisor) Consult(ctx context.Context, taskID string, req Request) (*Response, error) {
	if !a.cfg.Enabled {
		return a.fallback(taskID, req)
	}

	maxSession := a.cfg.MaxCallsPerSession
	if maxSession <= 0 {
		maxSession = 5
	}
	maxTask := a.cfg.MaxCallsPerTask
	if maxTask <= 0 {
		maxTask = 2
	}

	if a.sessionCalls >= maxSession {
		return a.fallback(taskID, req)
	}
	if a.taskCalls[taskID] >= maxTask {
		return a.fallback(taskID, req)
	}

	if a.runner == nil {
		return a.fallback(taskID, req)
	}

	prompt := buildAdvisorPrompt(req)

	timeout := 30 * time.Second
	if a.cfg.Timeout != "" {
		if d, err := time.ParseDuration(a.cfg.Timeout); err == nil {
			timeout = d
		}
	}

	advisorCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := a.runner.Run(advisorCtx, taskID+"-advisor", a.cfg.Runtime, a.cfg.Model, prompt)
	a.sessionCalls++
	a.taskCalls[taskID]++

	if err != nil {
		return a.fallback(taskID, req)
	}

	resp := parseAdvisorOutput(output)
	return resp, nil
}

func (a *Advisor) SessionCalls() int {
	return a.sessionCalls
}

func (a *Advisor) fallback(taskID string, req Request) (*Response, error) {
	contextPath := filepath.Join(a.taskDir, taskID, "advisor-context.yaml")
	_ = os.MkdirAll(filepath.Dir(contextPath), 0o755)

	data, _ := yaml.Marshal(req)
	_ = os.WriteFile(contextPath, data, 0o644)

	return &Response{
		Action:     ActionEscalateToHuman,
		Detail:     fmt.Sprintf("Advisor context written to %s. Review and respond with: baton respond %s \"<guidance>\"", contextPath, taskID),
		Confidence: "low",
	}, nil
}

func buildAdvisorPrompt(req Request) string {
	var b strings.Builder

	b.WriteString("You are a strategic advisor for a multi-phase pipeline.\n")
	b.WriteString("A worker is stuck. Analyze the situation and recommend ONE action.\n\n")

	b.WriteString("[TASK]\n")
	b.WriteString(req.Spec)
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "[CURRENT STATE]\n")
	fmt.Fprintf(&b, "Phase: %d (%s)\n", req.Phase, req.PhaseName)
	fmt.Fprintf(&b, "Role: %s\n", req.Role)
	fmt.Fprintf(&b, "L1 attempts used: %d\n", req.L1Attempts)
	fmt.Fprintf(&b, "L2 cycles used: %d\n", req.L2Cycles)
	fmt.Fprintf(&b, "Loop detected: %v\n\n", req.LoopDetected)

	if req.Scratchpad != "" {
		b.WriteString("[SCRATCHPAD — What was tried]\n")
		b.WriteString(req.Scratchpad)
		b.WriteString("\n\n")
	}

	if len(req.OutputTails) > 0 {
		b.WriteString("[RECENT OUTPUT TAILS]\n")
		for i, tail := range req.OutputTails {
			fmt.Fprintf(&b, "--- Attempt %d ---\n%s\n", i+1, tail)
		}
		b.WriteString("\n")
	}

	if len(req.FilesChanged) > 0 {
		b.WriteString("[FILES CHANGED SO FAR]\n")
		for _, f := range req.FilesChanged {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	b.WriteString("[RESPOND WITH EXACTLY ONE ACTION]\n")
	b.WriteString("Format your response as:\n")
	b.WriteString("ACTION: retry_with_hint | skip_phase | escalate_to_human | modify_constraints\n")
	b.WriteString("CONFIDENCE: high | medium | low\n")
	b.WriteString("DETAIL: <specific guidance or rationale>\n\n")
	b.WriteString("- retry_with_hint: Suggest a specific different approach for the next attempt\n")
	b.WriteString("- skip_phase: This phase cannot complete and should be skipped (justify why)\n")
	b.WriteString("- escalate_to_human: The issue requires human judgment (explain what's needed)\n")
	b.WriteString("- modify_constraints: A task constraint is causing the blockage (specify which)\n")

	return b.String()
}

func parseAdvisorOutput(output []string) *Response {
	resp := &Response{
		Action:     ActionEscalateToHuman,
		Confidence: "low",
	}

	full := strings.Join(output, "\n")

	for _, line := range output {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "ACTION:") {
			action := strings.TrimSpace(strings.TrimPrefix(line, "ACTION:"))
			switch action {
			case ActionRetryWithHint, ActionSkipPhase, ActionEscalateToHuman, ActionModifyConstraints:
				resp.Action = action
			}
		}

		if strings.HasPrefix(line, "CONFIDENCE:") {
			conf := strings.TrimSpace(strings.TrimPrefix(line, "CONFIDENCE:"))
			switch conf {
			case "high", "medium", "low":
				resp.Confidence = conf
			}
		}

		if strings.HasPrefix(line, "DETAIL:") {
			resp.Detail = strings.TrimSpace(strings.TrimPrefix(line, "DETAIL:"))
		}
	}

	if resp.Detail == "" {
		resp.Detail = full
	}

	return resp
}
