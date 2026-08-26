//go:build acplive

// Live end-to-end check against a real ACP agent. Excluded from the default
// build because it needs the binary installed and spends real tokens.
//
//	go test -tags acplive ./internal/acp/ -run TestLiveOpenCode -v
package acp

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/transport"
)

func TestLiveOpenCode(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not installed")
	}
	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"opencode": {Command: "opencode", Protocol: config.ProtocolACP, Args: []string{"acp"}},
	}}
	tr := New(cfg, func(f string, a ...any) { t.Logf(f, a...) })
	res, err := tr.Execute(context.Background(), transport.Request{
		TaskID: "live", RuntimeName: "opencode", Prompt: "Reply with exactly this line and nothing else: BATON:C:setup:done:ok",
		Liveness: transport.LivenessConfig{AbsoluteTimeout: 120 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	t.Logf("status=%s detail=%s", res.Status, res.ErrorDetail)
	t.Logf("events=%d output=%d", len(res.Events), len(res.Output))
	if res.Usage != nil {
		t.Logf("usage: in=%d out=%d total=%d", res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
	}
	t.Logf("caps=%+v", tr.Capabilities("opencode"))
	for _, l := range res.Output {
		t.Logf("  | %s", l)
	}
}

// Probe must establish real capabilities without invoking a model. If it ever
// starts costing a turn, preflight becomes too expensive to run.
func TestLiveProbeCostsNoTurn(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not installed")
	}
	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"opencode": {Command: "opencode", Protocol: config.ProtocolACP, Args: []string{"acp"}},
	}}
	tr := New(cfg, func(f string, a ...any) { t.Logf(f, a...) })

	start := time.Now()
	caps, err := tr.Probe(context.Background(), "opencode")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe took %s, caps=%+v", time.Since(start), caps)

	if !caps.Probed {
		t.Error("Probed=false after a successful probe")
	}
	if caps.ToolRestriction == transport.RestrictNone {
		t.Errorf("ToolRestriction=%q, want OpenCode's mode toggle to be discovered", caps.ToolRestriction)
	}
	if !caps.ModelSelect {
		t.Error("ModelSelect=false; OpenCode exposes a model config option")
	}
}
