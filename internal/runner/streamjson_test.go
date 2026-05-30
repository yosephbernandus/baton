package runner

import (
	"testing"
)

func TestParseStreamJSON_SystemInit(t *testing.T) {
	line := `{"type":"system","subtype":"init","cwd":"/tmp","session_id":"abc","tools":[]}`
	display, result, isResult, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[init] session started" {
		t.Errorf("display=%q", display)
	}
	if result != "" || isResult {
		t.Error("should not be a result event")
	}
}

func TestParseStreamJSON_SystemHook(t *testing.T) {
	line := `{"type":"system","subtype":"hook","hook_name":"PreToolUse","hook_action":"approve"}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "" {
		t.Errorf("hook events should be skipped, got %q", display)
	}
}

func TestParseStreamJSON_SystemUnknownSubtype(t *testing.T) {
	line := `{"type":"system","subtype":"custom_thing"}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[system] custom_thing" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"src/main.go"}}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[tool] Read src/main.go" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantToolUseLongPath(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/Users/user/project/internal/runner/runner.go"}}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[tool] Edit .../internal/runner/runner.go" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantToolUseBash(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[tool] Bash go test ./..." {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantToolUseNoArgs(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"CustomTool","input":{}}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[tool] CustomTool" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"I'll fix the bug in the authentication module."}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "I'll fix the bug in the authentication module." {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantTextFull(t *testing.T) {
	longText := ""
	for i := 0; i < 200; i++ {
		longText += "x"
	}
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + longText + `"}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(display) != 200 {
		t.Errorf("expected full 200 chars, got len=%d", len(display))
	}
}

func TestParseStreamJSON_AssistantThinking(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Let me analyze this..."}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[thinking] Let me analyze this..." {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantThinkingAndTool(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"checking file"},{"type":"tool_use","name":"Read","input":{"file_path":"foo.go"}}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[thinking] checking file\n[tool] Read foo.go" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantThinkingAndText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Here is the fix."}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[thinking] hmm\nHere is the fix." {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_AssistantEmpty(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "" {
		t.Errorf("expected empty display, got %q", display)
	}
}

func TestParseStreamJSON_AssistantMultipleTools(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}},{"type":"tool_use","name":"Read","input":{"file_path":"b.go"}}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[tool] Read a.go\n[tool] Read b.go" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_ResultSuccess(t *testing.T) {
	line := `{"type":"result","subtype":"success","duration_ms":2991,"result":"Task completed successfully","total_cost_usd":0.06}`
	display, result, isResult, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !isResult {
		t.Fatal("expected isResult=true")
	}
	if result != "Task completed successfully" {
		t.Errorf("result=%q", result)
	}
	if display != "[done] cost=$0.06 duration=3.0s" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_ResultError(t *testing.T) {
	line := `{"type":"result","subtype":"error","result":"rate limit exceeded","total_cost_usd":0.01,"duration_ms":500}`
	display, result, isResult, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !isResult {
		t.Fatal("expected isResult=true")
	}
	if result != "rate limit exceeded" {
		t.Errorf("result=%q", result)
	}
	if display != "[error] rate limit exceeded" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_ResultUnknownSubtype(t *testing.T) {
	line := `{"type":"result","subtype":"partial","result":"some output","duration_ms":100}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[result] partial" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_SystemHookStarted(t *testing.T) {
	line := `{"type":"system","subtype":"hook_started","hook_id":"abc","hook_name":"PreToolUse"}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "" {
		t.Errorf("hook_started should be skipped, got %q", display)
	}
}

func TestParseStreamJSON_SystemHookResponse(t *testing.T) {
	line := `{"type":"system","subtype":"hook_response","hook_id":"abc","hook_name":"PreToolUse"}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "" {
		t.Errorf("hook_response should be skipped, got %q", display)
	}
}

func TestParseStreamJSON_UserEvent(t *testing.T) {
	line := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_abc","type":"tool_result","content":"file contents"}]}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "" {
		t.Errorf("user events should be skipped, got %q", display)
	}
}

func TestParseStreamJSON_RateLimitEvent(t *testing.T) {
	line := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1780272000}}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[rate limited]" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_UnknownType(t *testing.T) {
	line := `{"type":"custom_extension","data":"something"}`
	display, _, _, ok := parseStreamJSON(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if display != "[custom_extension]" {
		t.Errorf("display=%q", display)
	}
}

func TestParseStreamJSON_InvalidJSON(t *testing.T) {
	_, _, _, ok := parseStreamJSON("this is not json")
	if ok {
		t.Fatal("expected ok=false for non-JSON input")
	}
}

func TestParseStreamJSON_EmptyObject(t *testing.T) {
	_, _, _, ok := parseStreamJSON("{}")
	if ok {
		t.Fatal("expected ok=false for empty object (no type field)")
	}
}

func TestParseStreamJSON_EmptyString(t *testing.T) {
	_, _, _, ok := parseStreamJSON("")
	if ok {
		t.Fatal("expected ok=false for empty string")
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"src/main.go", "src/main.go"},
		{"a/b/c", "a/b/c"},
		{"a/b/c/d", ".../b/c/d"},
		{"/Users/user/project/internal/runner/runner.go", ".../internal/runner/runner.go"},
		{"file.go", "file.go"},
	}
	for _, tt := range tests {
		got := shortenPath(tt.input)
		if got != tt.want {
			t.Errorf("shortenPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world this is long", 10, "hello w..."},
		{"ab", 2, "ab"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"abcdef", 5, "ab..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncateStr(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}
