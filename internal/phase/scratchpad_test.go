package phase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScratchpadAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	sp := NewScratchpad(dir, "test-task")

	if err := sp.AppendAttempt(1, []string{"tried X", "X failed"}, "build error"); err != nil {
		t.Fatal(err)
	}
	if err := sp.AppendAttempt(2, []string{"tried Y"}, "test failure"); err != nil {
		t.Fatal(err)
	}

	content, err := sp.Read()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "## Attempt 1") {
		t.Error("missing Attempt 1 header")
	}
	if !strings.Contains(content, "## Attempt 2") {
		t.Error("missing Attempt 2 header")
	}
	if !strings.Contains(content, "- tried X") {
		t.Error("missing note 'tried X'")
	}
	if !strings.Contains(content, "Failed — build error") {
		t.Error("missing fail reason")
	}
}

func TestScratchpadReadEmpty(t *testing.T) {
	dir := t.TempDir()
	sp := NewScratchpad(dir, "nonexistent")

	content, err := sp.Read()
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}
}

func TestScratchpadForPrompt(t *testing.T) {
	dir := t.TempDir()
	sp := NewScratchpad(dir, "test-task")

	if err := sp.AppendAttempt(1, []string{"tried X"}, "failed"); err != nil {
		t.Fatal(err)
	}

	prompt := sp.ForPrompt()
	if !strings.HasPrefix(prompt, "[SCRATCHPAD -- PREVIOUS ATTEMPTS]") {
		t.Error("missing scratchpad header")
	}
	if !strings.Contains(prompt, "tried X") {
		t.Error("missing note in prompt")
	}
	if !strings.Contains(prompt, "avoid repeating") {
		t.Error("missing instruction footer")
	}
}

func TestScratchpadForPromptEmpty(t *testing.T) {
	dir := t.TempDir()
	sp := NewScratchpad(dir, "nonexistent")

	prompt := sp.ForPrompt()
	if prompt != "" {
		t.Errorf("expected empty, got %q", prompt)
	}
}

func TestScratchpadAppendNoNotes(t *testing.T) {
	dir := t.TempDir()
	sp := NewScratchpad(dir, "test-task")

	if err := sp.AppendAttempt(1, nil, "crashed"); err != nil {
		t.Fatal(err)
	}

	content, err := sp.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "Failed — crashed") {
		t.Error("missing fail reason")
	}
	if strings.Contains(content, "**Notes:**") {
		t.Error("should not have Notes section with nil notes")
	}
}

func TestScratchpadTruncation(t *testing.T) {
	dir := t.TempDir()
	sp := NewScratchpad(dir, "test-task")

	// Write a lot of data to exceed 4KB
	bigNote := strings.Repeat("x", 200)
	for i := 1; i <= 30; i++ {
		if err := sp.AppendAttempt(i, []string{bigNote}, "failed"); err != nil {
			t.Fatal(err)
		}
	}

	content, err := sp.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(content) <= 4096 {
		t.Skip("content not large enough to test truncation")
	}

	prompt := sp.ForPrompt()
	// Prompt should be truncated — header + content + footer within reasonable bounds
	// The content portion should be <= 4096 bytes
	if !strings.Contains(prompt, "[SCRATCHPAD -- PREVIOUS ATTEMPTS]") {
		t.Error("missing header after truncation")
	}
	if !strings.Contains(prompt, "avoid repeating") {
		t.Error("missing footer after truncation")
	}
}

func TestScratchpadCreatesDir(t *testing.T) {
	dir := t.TempDir()
	taskID := "deep/nested/task"
	sp := NewScratchpad(dir, taskID)

	if err := sp.AppendAttempt(1, []string{"note"}, "reason"); err != nil {
		t.Fatal(err)
	}

	expectedPath := filepath.Join(dir, taskID, "scratchpad.md")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("scratchpad file not created at %s: %v", expectedPath, err)
	}
}
