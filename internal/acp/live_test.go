//go:build acplive

// Live end-to-end check against a real ACP agent. Excluded from the default
// build because it needs the binary installed and spends real tokens.
//
//	go test -tags acplive ./internal/acp/ -run TestLiveOpenCode -v
package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// The bug this guards: tool calls report the files they name, and a change made
// through a shell command names none. Role boundary verification runs on
// FilesChanged, so if it only carried tool reports, an agent could edit through
// a shell and be recorded as having touched nothing.
func TestLiveShellEditIsStillAttributed(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	// An isolated repo: the transport snapshots the process working directory,
	// so this must not run against baton's own tree.
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "target.txt")
	run("commit", "-qm", "seed")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"opencode": {Command: "opencode", Protocol: config.ProtocolACP, Args: []string{"acp"}},
	}}
	tr := New(cfg, func(f string, a ...any) { t.Logf(f, a...) })

	res, err := tr.Execute(context.Background(), transport.Request{
		TaskID: "live-shell", RuntimeName: "opencode",
		Prompt: "Using a shell command only (do not use any file editing tool), " +
			"append the line CHANGED to target.txt in the current directory.",
		Liveness: transport.LivenessConfig{AbsoluteTimeout: 180 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "CHANGED") {
		t.Skipf("agent did not modify the file, nothing to attribute; content=%q", content)
	}

	var reportedByTools int
	for _, ev := range res.Events {
		if ev.ToolCall != nil {
			reportedByTools += len(ev.ToolCall.Locations)
		}
	}
	t.Logf("FilesChanged=%v (tool calls named %d paths)", res.FilesChanged, reportedByTools)

	if len(res.FilesChanged) == 0 {
		t.Fatal("file changed on disk but FilesChanged is empty: role verification would be blind")
	}
	if !slices.Contains(res.FilesChanged, "target.txt") {
		t.Errorf("FilesChanged=%v, want the repo-relative target.txt", res.FilesChanged)
	}
}

// The field is configId, not optionId. Getting it wrong was silent: OpenCode
// answered -32602 on stderr, the option kept its previous value, and baton
// carried on believing it had applied a restriction it had not. A read-only
// phase ran with edit tools intact because of exactly this.
//
// Drive a real agent with a read-only role and assert nothing was rejected.
func TestLiveReadOnlyModeIsAccepted(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not installed")
	}

	var mu sync.Mutex
	var logged []string
	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"opencode": {Command: "opencode", Protocol: config.ProtocolACP, Args: []string{"acp"}},
	}}
	tr := New(cfg, func(f string, a ...any) {
		mu.Lock()
		logged = append(logged, fmt.Sprintf(f, a...))
		mu.Unlock()
		t.Logf(f, a...)
	})

	res, err := tr.Execute(context.Background(), transport.Request{
		TaskID: "live-mode", RuntimeName: "opencode",
		Prompt: "Reply with exactly this line and nothing else: BATON:C:setup:done:ok",
		// A read-only boundary, so the transport asks for a mode that
		// withholds edit tools.
		AllowedTools: []string{"Read", "Grep", "Glob"},
		Liveness:     transport.LivenessConfig{AbsoluteTimeout: 120 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("status=%q detail=%q", res.Status, res.ErrorDetail)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, line := range logged {
		if strings.Contains(line, "-32602") || strings.Contains(line, "Invalid params") {
			t.Fatalf("agent rejected a config option: %q", line)
		}
		if strings.Contains(line, "no read-only mode") {
			t.Fatalf("read-only mode was not applied: %q", line)
		}
	}
}

// A tool_call reports locations for reads as much as for writes. Taking them all
// attributed every file a phase merely looked at, so a read-only role failed its
// own boundary check for reading the context files it was handed — which is what
// every read-only phase does.
func TestLiveReadingIsNotAChange(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not installed")
	}
	dir := acpGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "more"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	cfg := &config.Config{Runtimes: map[string]config.RuntimeConfig{
		"opencode": {Command: "opencode", Protocol: config.ProtocolACP, Args: []string{"acp"}},
	}}
	tr := New(cfg, func(f string, a ...any) { t.Logf(f, a...) })

	res, err := tr.Execute(context.Background(), transport.Request{
		TaskID: "read-only", RuntimeName: "opencode",
		Prompt: "Read target.txt and other.txt. Do not modify anything. " +
			"Then reply with exactly: BATON:C:setup:done:read",
		AllowedTools: []string{"Read", "Grep", "Glob"},
		Liveness:     transport.LivenessConfig{AbsoluteTimeout: 150 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var readLocations int
	for _, ev := range res.Events {
		if ev.ToolCall != nil && ev.ToolCall.Kind == "read" {
			readLocations += len(ev.ToolCall.Locations)
		}
	}
	if readLocations == 0 {
		t.Skip("agent reported no read locations; nothing to misattribute")
	}
	t.Logf("read tool calls named %d paths; FilesChanged=%v", readLocations, res.FilesChanged)

	if len(res.FilesChanged) != 0 {
		t.Errorf("FilesChanged=%v, want none: the turn only read", res.FilesChanged)
	}
}
