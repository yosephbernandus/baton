package runner

import (
	"encoding/json"
	"fmt"
	"strings"
)

type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	Message struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`

	Result       string  `json:"result"`
	DurationMs   int     `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

type contentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

func parseStreamJSON(line string) (displayLine, resultText string, isResult bool, ok bool) {
	var ev streamEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return "", "", false, false
	}
	if ev.Type == "" {
		return "", "", false, false
	}

	switch ev.Type {
	case "system":
		return formatSystemEvent(&ev), "", false, true
	case "assistant":
		return formatAssistantEvent(&ev), "", false, true
	case "result":
		display := formatResultEvent(&ev)
		return display, ev.Result, true, true
	case "user":
		return "", "", false, true
	case "rate_limit_event":
		return "[rate limited]", "", false, true
	default:
		return fmt.Sprintf("[%s]", ev.Type), "", false, true
	}
}

func formatSystemEvent(ev *streamEvent) string {
	switch ev.Subtype {
	case "init":
		return "[init] session started"
	case "hook", "hook_started", "hook_response",
		"api_retry", "thinking_tokens":
		return ""
	default:
		return fmt.Sprintf("[system] %s", ev.Subtype)
	}
}

func formatAssistantEvent(ev *streamEvent) string {
	var parts []string

	for _, block := range ev.Message.Content {
		switch block.Type {
		case "tool_use":
			parts = append(parts, formatToolUse(block.Name, block.Input))
		case "text":
			if t := strings.TrimSpace(block.Text); t != "" {
				parts = append(parts, t)
			}
		case "thinking":
			if t := strings.TrimSpace(block.Thinking); t != "" {
				parts = append(parts, "[thinking] "+t)
			}
		}
	}

	return strings.Join(parts, "\n")
}

func formatToolUse(name string, input json.RawMessage) string {
	var args map[string]interface{}
	_ = json.Unmarshal(input, &args)

	switch name {
	case "Read":
		if p, ok := args["file_path"].(string); ok {
			return fmt.Sprintf("[tool] Read %s", shortenPath(p))
		}
	case "Edit":
		if p, ok := args["file_path"].(string); ok {
			return fmt.Sprintf("[tool] Edit %s", shortenPath(p))
		}
	case "Write":
		if p, ok := args["file_path"].(string); ok {
			return fmt.Sprintf("[tool] Write %s", shortenPath(p))
		}
	case "Bash":
		if cmd, ok := args["command"].(string); ok {
			return fmt.Sprintf("[tool] Bash %s", truncateStr(cmd, 60))
		}
	case "Grep", "Glob":
		if pat, ok := args["pattern"].(string); ok {
			return fmt.Sprintf("[tool] %s %s", name, truncateStr(pat, 40))
		}
	}
	return fmt.Sprintf("[tool] %s", name)
}

func formatResultEvent(ev *streamEvent) string {
	switch ev.Subtype {
	case "success":
		cost := fmt.Sprintf("$%.2f", ev.TotalCostUSD)
		dur := fmt.Sprintf("%.1fs", float64(ev.DurationMs)/1000.0)
		return fmt.Sprintf("[done] cost=%s duration=%s", cost, dur)
	case "error":
		return fmt.Sprintf("[error] %s", truncateStr(ev.Result, 100))
	default:
		return fmt.Sprintf("[result] %s", ev.Subtype)
	}
}

func shortenPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 3 {
		return p
	}
	return ".../" + strings.Join(parts[len(parts)-3:], "/")
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
