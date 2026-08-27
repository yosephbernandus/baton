package config

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
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

// agents.examples.yaml is documentation users copy verbatim, so it has to parse
// as a real config. Nothing checked that, and the role_models section it was
// missing is exactly the one that broke every scaffolded project.
func TestShippedExampleConfigParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "agents.examples.yaml"))
	if err != nil {
		t.Skipf("example config not readable from here: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("agents.examples.yaml does not parse: %v", err)
	}

	if len(cfg.Runtimes) == 0 {
		t.Error("example config defines no runtimes")
	}
	for role, rm := range cfg.RoleModels {
		if rm.Runtime == "" || rm.Model == "" {
			t.Errorf("role %q = %+v: resolveRoleRuntime needs both a runtime and a model", role, rm)
		}
		if _, ok := cfg.Runtimes[rm.Runtime]; !ok {
			t.Errorf("role %q names runtime %q, which the example does not define", role, rm.Runtime)
		}
	}
}

// The shorthand and the explicit form must mean the same thing.
func TestRoleModelShorthandMatchesExplicitForm(t *testing.T) {
	var short struct {
		RoleModels map[string]RoleModelConfig `yaml:"role_models"`
	}
	if err := yaml.Unmarshal([]byte("role_models:\n  lead: claude-code\n"), &short); err != nil {
		t.Fatalf("shorthand does not parse: %v", err)
	}
	if got := short.RoleModels["lead"]; got.Runtime != "claude-code" || got.Model != ModelAuto {
		t.Errorf("shorthand = %+v, want runtime claude-code and model %q", got, ModelAuto)
	}

	var explicit struct {
		RoleModels map[string]RoleModelConfig `yaml:"role_models"`
	}
	if err := yaml.Unmarshal([]byte("role_models:\n  lead:\n    runtime: claude-code\n"), &explicit); err != nil {
		t.Fatalf("explicit form does not parse: %v", err)
	}
	if got := explicit.RoleModels["lead"]; got.Model != ModelAuto {
		t.Errorf("explicit form with no model = %+v, want the model defaulted to %q", got, ModelAuto)
	}
}

func TestRoleModelExplicitModelIsKept(t *testing.T) {
	var v struct {
		RoleModels map[string]RoleModelConfig `yaml:"role_models"`
	}
	if err := yaml.Unmarshal([]byte("role_models:\n  developer: {runtime: opencode, model: kimi}\n"), &v); err != nil {
		t.Fatal(err)
	}
	if got := v.RoleModels["developer"]; got.Runtime != "opencode" || got.Model != "kimi" {
		t.Errorf("got %+v, want opencode/kimi", got)
	}
}
