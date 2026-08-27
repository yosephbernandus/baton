package config

import (
	"context"
	"slices"
	"testing"
	"time"
)

func acpConfig() *Config {
	return &Config{Runtimes: map[string]RuntimeConfig{
		"acp-one": {Command: "echo", Protocol: ProtocolACP, Args: []string{"acp"}},
	}}
}

// An ACP runtime carries no prompt on its command line — the prompt travels over
// the protocol — so the flag checks do not apply. Reporting one as misconfigured
// failed a runtime that had just run three pipelines, and made `baton doctor`
// exit non-zero on a working setup.
func TestACPRuntimeNeedsNoPromptFlag(t *testing.T) {
	diag := acpConfig().DiagnoseRuntime("acp-one")
	if !diag.Exists {
		t.Fatal("command not found")
	}
	if !diag.ArgsValid {
		t.Errorf("ArgsValid=false (%q), want valid: an ACP runtime has no prompt flag", diag.ArgsError)
	}
	if diag.Protocol != ProtocolACP {
		t.Errorf("Protocol=%q, want %q", diag.Protocol, ProtocolACP)
	}
	if !slices.Equal(diag.Args, []string{"acp"}) {
		t.Errorf("Args=%v, want the args the runtime is invoked with", diag.Args)
	}
}

// An exec runtime with nothing to carry a prompt is still misconfigured.
func TestExecRuntimeStillNeedsAPromptPath(t *testing.T) {
	cfg := &Config{Runtimes: map[string]RuntimeConfig{
		"broken": {Command: "echo"},
	}}
	diag := cfg.DiagnoseRuntime("broken")
	if diag.ArgsValid {
		t.Error("ArgsValid=true, want false: an exec runtime needs a prompt flag or positional")
	}
}

// Sending a text prompt to an ACP agent is not a probe: it reads JSON-RPC on
// stdin and would sit there until the timeout. The handshake belongs to the
// transport, which this package cannot import.
func TestProbeRefusesToTextPromptAnACPRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	pr := acpConfig().ProbeRuntime(ctx, "acp-one")
	if pr.Error == "" {
		t.Fatal("expected a refusal rather than a text-prompt probe")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s, want an immediate refusal rather than a spawn", elapsed)
	}
}

func TestUnknownRuntimeIsStillReported(t *testing.T) {
	diag := acpConfig().DiagnoseRuntime("nope")
	if diag.ArgsError != "runtime not found in config" {
		t.Errorf("ArgsError=%q", diag.ArgsError)
	}
}

// A missing binary fails whatever protocol it claims.
func TestACPRuntimeWithMissingCommandStillFails(t *testing.T) {
	cfg := &Config{Runtimes: map[string]RuntimeConfig{
		"gone": {Command: "definitely-not-installed-xyz", Protocol: ProtocolACP, Args: []string{"acp"}},
	}}
	diag := cfg.DiagnoseRuntime("gone")
	if diag.Exists {
		t.Fatal("Exists=true for a command that is not installed")
	}
	if diag.ArgsError != "command not found" {
		t.Errorf("ArgsError=%q, want %q", diag.ArgsError, "command not found")
	}
}
