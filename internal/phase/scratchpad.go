package phase

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Scratchpad struct {
	path string
}

func NewScratchpad(taskDir, taskID string) *Scratchpad {
	return &Scratchpad{
		path: filepath.Join(taskDir, taskID, "scratchpad.md"),
	}
}

func (s *Scratchpad) AppendAttempt(attempt int, notes []string, failReason string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "## Attempt %d [%s]\n\n", attempt, time.Now().Format(time.RFC3339))

	if len(notes) > 0 {
		fmt.Fprintln(f, "**Notes:**")
		for _, n := range notes {
			fmt.Fprintf(f, "- %s\n", n)
		}
		fmt.Fprintln(f)
	}

	if failReason != "" {
		fmt.Fprintf(f, "**Result:** Failed — %s\n\n", failReason)
	} else {
		fmt.Fprint(f, "**Result:** Failed\n\n")
	}

	return nil
}

func (s *Scratchpad) Read() (string, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Scratchpad) ForPrompt() string {
	content, err := s.Read()
	if err != nil || content == "" {
		return ""
	}

	// Truncate to ~4KB to avoid blowing up context
	const maxBytes = 4096
	if len(content) > maxBytes {
		lines := strings.Split(content, "\n")
		var trimmed []string
		size := 0
		for i := len(lines) - 1; i >= 0; i-- {
			lineSize := len(lines[i]) + 1
			if size+lineSize > maxBytes {
				break
			}
			trimmed = append([]string{lines[i]}, trimmed...)
			size += lineSize
		}
		content = strings.Join(trimmed, "\n")
	}

	var b strings.Builder
	b.WriteString("[SCRATCHPAD -- PREVIOUS ATTEMPTS]\n")
	b.WriteString(content)
	b.WriteString("\nUse these notes to avoid repeating failed approaches. Build on what worked.\n")
	return b.String()
}
