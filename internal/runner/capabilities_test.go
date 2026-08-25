package runner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/proto"
	"github.com/yosephbernandus/baton/internal/transport"
)

func capsFor(t *testing.T, rt config.RuntimeConfig) transport.Caps {
	t.Helper()
	r := New(&config.Config{
		Runtimes: map[string]config.RuntimeConfig{"mock": rt},
	}, nil, nil)
	return r.Capabilities("mock")
}

func TestCapabilitiesPerToolWhenRuntimeDeclaresFlag(t *testing.T) {
	caps := capsFor(t, config.RuntimeConfig{
		Command:         "echo",
		ToolRestriction: &config.ToolRestriction{Flag: "--allowedTools", Format: "comma-separated"},
	})
	if caps.ToolRestriction != transport.RestrictPerTool {
		t.Errorf("ToolRestriction=%q, want %q", caps.ToolRestriction, transport.RestrictPerTool)
	}
}

func TestCapabilitiesNoneWhenRuntimeHasNoFlag(t *testing.T) {
	caps := capsFor(t, config.RuntimeConfig{Command: "echo"})
	if caps.ToolRestriction != transport.RestrictNone {
		t.Errorf("ToolRestriction=%q, want %q", caps.ToolRestriction, transport.RestrictNone)
	}
}

// An empty flag string is a misconfiguration, not a capability. Reporting
// per-tool here would let the gateway pass a check the transport cannot honour.
func TestCapabilitiesNoneWhenFlagIsEmpty(t *testing.T) {
	caps := capsFor(t, config.RuntimeConfig{
		Command:         "echo",
		ToolRestriction: &config.ToolRestriction{Flag: "", Format: "comma-separated"},
	})
	if caps.ToolRestriction != transport.RestrictNone {
		t.Errorf("ToolRestriction=%q, want %q", caps.ToolRestriction, transport.RestrictNone)
	}
}

func TestCapabilitiesModelSelectFollowsModelFlag(t *testing.T) {
	if caps := capsFor(t, config.RuntimeConfig{Command: "echo", ModelFlag: "--model"}); !caps.ModelSelect {
		t.Error("ModelSelect=false, want true when the runtime declares a model flag")
	}
	if caps := capsFor(t, config.RuntimeConfig{Command: "echo"}); caps.ModelSelect {
		t.Error("ModelSelect=true, want false when the runtime declares no model flag")
	}
}

// Everything a structured protocol would report is absent here by construction:
// all this transport sees is stdout.
func TestCapabilitiesExecCannotSeeStructuredSignals(t *testing.T) {
	caps := capsFor(t, config.RuntimeConfig{
		Command:         "echo",
		ModelFlag:       "--model",
		ToolRestriction: &config.ToolRestriction{Flag: "--allowedTools", Format: "comma-separated"},
	})
	if caps.Permission {
		t.Error("Permission=true, want false")
	}
	if caps.Usage {
		t.Error("Usage=true, want false")
	}
	if caps.FileLocations {
		t.Error("FileLocations=true, want false")
	}
	if caps.Persistent {
		t.Error("Persistent=true, want false")
	}
}

func TestCapabilitiesUnknownRuntimeRestrictsNothing(t *testing.T) {
	r := New(&config.Config{Runtimes: map[string]config.RuntimeConfig{}}, nil, nil)
	caps := r.Capabilities("does-not-exist")
	if caps.ToolRestriction != transport.RestrictNone || caps.ModelSelect {
		t.Errorf("caps=%+v, want a zero-capability report", caps)
	}
}

// The pipeline hands the exec transport a tool boundary as intent; turning that
// into argv happens here. Drive a real subprocess and let it report the flags it
// actually received, so the translation is verified end to end rather than by
// re-calling the helper that produces it.
func TestExecuteTranslatesAllowedToolsIntoFlags(t *testing.T) {
	r, store, _ := setupTestRunner(t)
	r.cfg.Runtimes["mock"] = config.RuntimeConfig{
		Command:         "bash",
		PromptFlag:      "-c",
		Models:          []string{"default"},
		ToolRestriction: &config.ToolRestriction{Flag: "--allowedTools", Format: "comma-separated"},
	}

	taskID := "test-tool-flags"
	createTestTask(t, store, taskID)
	if err := os.MkdirAll(".baton/tasks/"+taskID, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(".baton/tasks/" + taskID)

	result, err := r.Execute(context.Background(), transport.Request{
		TaskID:       taskID,
		RuntimeName:  "mock",
		Model:        "default",
		Prompt:       `echo "BATON:N:argv=$0 $1"; echo "BATON:C:setup:done"`,
		AllowedTools: []string{"Read", "Grep"},
		Liveness:     LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	notes := proto.Notes(result.Events)
	if len(notes) != 1 {
		t.Fatalf("notes=%v, want one argv report", notes)
	}
	if !strings.Contains(notes[0], "--allowedTools") {
		t.Errorf("argv %q missing the restriction flag", notes[0])
	}
	if !strings.Contains(notes[0], "Read,Grep") {
		t.Errorf("argv %q missing the comma-separated tool list", notes[0])
	}
}

// A runtime with no restriction flag must not gain phantom argv. The boundary is
// still declared; the transport simply cannot honour it, which Capabilities says.
func TestExecuteAddsNoFlagsWhenRuntimeCannotRestrict(t *testing.T) {
	r, store, _ := setupTestRunner(t)

	taskID := "test-no-tool-flags"
	createTestTask(t, store, taskID)
	if err := os.MkdirAll(".baton/tasks/"+taskID, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(".baton/tasks/" + taskID)

	result, err := r.Execute(context.Background(), transport.Request{
		TaskID:      taskID,
		RuntimeName: "mock",
		Model:       "default",
		// bash -c names $0 "bash" when no extra arguments follow the script,
		// so an empty $1 is what "nothing was appended" looks like here.
		Prompt:       `echo "BATON:N:argv=[$1]"; echo "BATON:C:setup:done"`,
		AllowedTools: []string{"Read", "Grep"},
		Liveness:     LivenessConfig{AbsoluteTimeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	notes := proto.Notes(result.Events)
	if len(notes) != 1 {
		t.Fatalf("notes=%v, want one argv report", notes)
	}
	if notes[0] != "argv=[]" {
		t.Errorf("argv %q, want no arguments appended", notes[0])
	}
	if caps := r.Capabilities("mock"); caps.ToolRestriction != transport.RestrictNone {
		t.Errorf("ToolRestriction=%q, want %q", caps.ToolRestriction, transport.RestrictNone)
	}
}
