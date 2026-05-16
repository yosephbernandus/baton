package config

import (
	"testing"
)

func TestDiagnoseRuntimeExists(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"test": {
				Command:    "echo",
				PromptFlag: "-p",
				Models:     []string{"m1", "m2"},
			},
		},
	}

	result := cfg.DiagnoseRuntime("test")
	if !result.Exists {
		t.Fatal("echo should exist")
	}
	if !result.ArgsValid {
		t.Fatalf("args should be valid, got error: %s", result.ArgsError)
	}
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}
}

func TestDiagnoseRuntimeNotFound(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"missing": {
				Command: "nonexistent-binary-xyz-12345",
			},
		},
	}

	result := cfg.DiagnoseRuntime("missing")
	if result.Exists {
		t.Fatal("should not exist")
	}
	if result.ArgsError != "command not found" {
		t.Fatalf("expected 'command not found', got %q", result.ArgsError)
	}
}

func TestDiagnoseRuntimeNotInConfig(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{},
	}

	result := cfg.DiagnoseRuntime("ghost")
	if result.Exists {
		t.Fatal("should not exist")
	}
	if result.ArgsError != "runtime not found in config" {
		t.Fatalf("expected 'runtime not found in config', got %q", result.ArgsError)
	}
}

func TestDiagnoseRuntimeNoPromptConfig(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"bare": {
				Command: "echo",
			},
		},
	}

	result := cfg.DiagnoseRuntime("bare")
	if !result.Exists {
		t.Fatal("echo should exist")
	}
	if result.ArgsValid {
		t.Fatal("should be invalid — no prompt_flag or positional")
	}
}

func TestDiagnoseRuntimePositional(t *testing.T) {
	cfg := &Config{
		Runtimes: map[string]RuntimeConfig{
			"oc": {
				Command:    "echo",
				ModelFlag:  "-m",
				Positional: []string{"run", "{{prompt}}"},
				Models:     []string{"kimi"},
			},
		},
	}

	result := cfg.DiagnoseRuntime("oc")
	if !result.Exists {
		t.Fatal("echo should exist")
	}
	if !result.ArgsValid {
		t.Fatalf("should be valid, got: %s", result.ArgsError)
	}
}
