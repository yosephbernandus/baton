package cmd

import "testing"

func TestGetNestedValue(t *testing.T) {
	data := map[string]interface{}{
		"orchestrator": map[string]interface{}{
			"runtime": "claude-code",
			"model":   "sonnet",
		},
		"defaults": map[string]interface{}{
			"runtime": "opencode",
		},
	}

	val, err := getNestedValue(data, "orchestrator.runtime")
	if err != nil {
		t.Fatal(err)
	}
	if val != "claude-code" {
		t.Errorf("expected claude-code, got %v", val)
	}

	val, err = getNestedValue(data, "defaults.runtime")
	if err != nil {
		t.Fatal(err)
	}
	if val != "opencode" {
		t.Errorf("expected opencode, got %v", val)
	}

	_, err = getNestedValue(data, "nonexistent.key")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestSetNestedValue(t *testing.T) {
	data := map[string]interface{}{
		"orchestrator": map[string]interface{}{
			"runtime": "claude-code",
		},
	}

	setNestedValue(data, "orchestrator.model", "opus")

	val, _ := getNestedValue(data, "orchestrator.model")
	if val != "opus" {
		t.Errorf("expected opus, got %v", val)
	}

	setNestedValue(data, "new.nested.key", "value")
	val, _ = getNestedValue(data, "new.nested.key")
	if val != "value" {
		t.Errorf("expected value, got %v", val)
	}
}
