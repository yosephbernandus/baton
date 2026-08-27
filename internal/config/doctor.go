package config

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type DiagnosticResult struct {
	Runtime     string
	Command     string
	CommandPath string
	Exists      bool
	ArgsValid   bool
	ArgsError   string
	Models      []string
	ProbeResult *ProbeResult

	// Protocol is the transport this runtime speaks, empty or "exec" for a
	// subprocess. Args is what an ACP runtime is invoked with.
	Protocol string
	Args     []string
}

type ProbeResult struct {
	ExitCode int
	Output   string
	Duration time.Duration
	Error    string
}

func (c *Config) DiagnoseRuntime(name string) DiagnosticResult {
	rt, ok := c.Runtimes[name]
	if !ok {
		return DiagnosticResult{
			Runtime:   name,
			ArgsError: "runtime not found in config",
		}
	}

	result := DiagnosticResult{
		Runtime:  name,
		Command:  rt.Command,
		Models:   rt.Models,
		Protocol: rt.Protocol,
	}

	path, err := exec.LookPath(rt.Command)
	if err != nil {
		result.Exists = false
		result.ArgsError = "command not found"
		return result
	}
	result.Exists = true
	result.CommandPath = path

	// An ACP runtime carries no prompt on its command line — the prompt travels
	// over the protocol — so the flag checks below do not apply to it. Its args
	// are passed verbatim, and whether it actually speaks ACP is a question only
	// a handshake can answer.
	if rt.Protocol == ProtocolACP {
		result.ArgsValid = true
		result.Args = rt.Args
		return result
	}

	var args []string
	for _, p := range rt.Positional {
		if p != "{{prompt}}" {
			args = append(args, p)
		} else if rt.PromptMode != "stdin" {
			args = append(args, "test-prompt")
		}
	}
	if rt.ModelFlag != "" && len(rt.Models) > 0 {
		args = append(args, rt.ModelFlag, rt.Models[0])
	}
	if rt.PromptFlag != "" && rt.PromptMode != "stdin" {
		args = append(args, rt.PromptFlag, "test-prompt")
	}
	args = append(args, rt.ExtraFlags...)

	result.ArgsValid = true
	if len(args) == 0 && rt.PromptFlag == "" && len(rt.Positional) == 0 {
		result.ArgsValid = false
		result.ArgsError = "no prompt_flag or positional configured"
	}

	return result
}

func (c *Config) ProbeRuntime(ctx context.Context, name string) ProbeResult {
	rt, ok := c.Runtimes[name]
	if !ok {
		return ProbeResult{Error: "runtime not found"}
	}

	// Sending a text prompt to an ACP agent is not a probe: it reads JSON-RPC
	// on stdin and would sit there until the timeout. Establishing whether an
	// ACP runtime works means completing a handshake, which lives in the
	// transport rather than here.
	if rt.Protocol == ProtocolACP {
		return ProbeResult{Error: "acp runtime: probe via the transport, not a text prompt"}
	}

	probePrompt := "respond with exactly: BATON_PROBE_OK"

	stdinMode := rt.PromptMode == "stdin"

	var args []string
	for _, p := range rt.Positional {
		if p == "{{prompt}}" {
			if !stdinMode {
				args = append(args, probePrompt)
			}
		} else {
			args = append(args, p)
		}
	}
	if rt.ModelFlag != "" && len(rt.Models) > 0 {
		args = append(args, rt.ModelFlag, rt.Models[0])
	}
	args = append(args, rt.ExtraFlags...)
	if rt.PromptFlag != "" && !stdinMode {
		args = append(args, rt.PromptFlag, probePrompt)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, rt.Command, args...)
	if stdinMode {
		cmd.Stdin = strings.NewReader(probePrompt)
	}

	start := time.Now()
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)

	result := ProbeResult{
		Output:   string(out),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
			return result
		}
	}

	// Check for probe marker — also accept variations LLMs commonly produce
	output := strings.ToUpper(result.Output)
	if !strings.Contains(output, "BATON_PROBE_OK") {
		result.Error = fmt.Sprintf("probe response missing BATON_PROBE_OK (exit %d)", result.ExitCode)
	}

	return result
}
