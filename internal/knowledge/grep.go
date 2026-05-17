package knowledge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type GrepResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
	Kind    string `json:"kind"` // definition, usage, unknown
}

type GrepOpts struct {
	Dir         string
	MaxResults  int
	FilePattern string // e.g. "*.go"
	IgnoreDirs  []string
}

func Grep(pattern string, opts GrepOpts) ([]GrepResult, error) {
	if opts.MaxResults == 0 {
		opts.MaxResults = 50
	}
	if opts.Dir == "" {
		opts.Dir = "."
	}

	if rgPath, err := exec.LookPath("rg"); err == nil {
		return grepWithRipgrep(rgPath, pattern, opts)
	}
	return grepWithBash(pattern, opts)
}

func grepWithRipgrep(rgPath, pattern string, opts GrepOpts) ([]GrepResult, error) {
	args := []string{
		"--line-number",
		"--no-heading",
		"--color=never",
		"--max-count", strconv.Itoa(opts.MaxResults),
		"--smart-case",
	}

	for _, d := range defaultIgnoreDirs(opts.IgnoreDirs) {
		args = append(args, "--glob", "!"+d)
	}

	if opts.FilePattern != "" {
		args = append(args, "--glob", opts.FilePattern)
	}

	args = append(args, pattern, opts.Dir)

	cmd := exec.Command(rgPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no matches
		}
		return nil, fmt.Errorf("ripgrep: %w", err)
	}

	return parseGrepOutput(opts.Dir, string(out), opts.MaxResults)
}

func grepWithBash(pattern string, opts GrepOpts) ([]GrepResult, error) {
	args := []string{"-rn", "--color=never"}

	for _, d := range defaultIgnoreDirs(opts.IgnoreDirs) {
		args = append(args, "--exclude-dir="+d)
	}

	if opts.FilePattern != "" {
		args = append(args, "--include="+opts.FilePattern)
	}

	args = append(args, pattern, opts.Dir)

	cmd := exec.Command("grep", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no matches
		}
		return nil, fmt.Errorf("grep: %w", err)
	}

	return parseGrepOutput(opts.Dir, string(out), opts.MaxResults)
}

func parseGrepOutput(baseDir, output string, max int) ([]GrepResult, error) {
	var results []GrepResult
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() && len(results) < max {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		file := parts[0]
		if rel, err := filepath.Rel(baseDir, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = rel
		}

		lineNum, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		content := strings.TrimSpace(parts[2])
		kind := classifyMatch(content)

		results = append(results, GrepResult{
			File:    file,
			Line:    lineNum,
			Content: content,
			Kind:    kind,
		})
	}

	return results, scanner.Err()
}

func classifyMatch(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "func ") ||
		strings.HasPrefix(trimmed, "type ") ||
		strings.HasPrefix(trimmed, "const ") ||
		strings.HasPrefix(trimmed, "var ") ||
		strings.HasPrefix(trimmed, "class ") ||
		strings.HasPrefix(trimmed, "def ") ||
		strings.HasPrefix(trimmed, "interface ") ||
		strings.HasPrefix(trimmed, "export ") {
		return "definition"
	}
	return "usage"
}

func defaultIgnoreDirs(extra []string) []string {
	defaults := []string{".git", "vendor", "node_modules", ".baton", "testdata"}
	seen := map[string]bool{}
	for _, d := range defaults {
		seen[d] = true
	}
	for _, d := range extra {
		if !seen[d] {
			defaults = append(defaults, d)
			seen[d] = true
		}
	}
	return defaults
}

func FormatGrepJSON(results []GrepResult) string {
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data)
}

func FormatGrepText(results []GrepResult) string {
	if len(results) == 0 {
		return "No matches found.\n"
	}

	var b strings.Builder
	for _, r := range results {
		tag := ""
		if r.Kind == "definition" {
			tag = " [def]"
		}
		fmt.Fprintf(&b, "%s:%d%s\n  %s\n", r.File, r.Line, tag, r.Content)
	}
	return b.String()
}
