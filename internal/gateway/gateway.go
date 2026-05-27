package gateway

import (
	"fmt"
	"strings"

	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

type Severity int

const (
	SeverityWarn  Severity = iota
	SeverityError
)

type Finding struct {
	Check    string
	Severity Severity
	Message  string
}

func (f Finding) String() string {
	level := "WARN"
	if f.Severity == SeverityError {
		level = "ERROR"
	}
	return fmt.Sprintf("[%s] %s: %s", level, f.Check, f.Message)
}

type Input struct {
	Config     *config.Config
	Spec       *spec.Spec
	Runtimes   []string
	Complexity string
	Mode       string // "run" or "pipeline"
}

type Report struct {
	Findings []Finding
}

func (r *Report) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r *Report) FormatStderr() string {
	if len(r.Findings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Gateway preflight:\n")
	for _, f := range r.Findings {
		b.WriteString("  ")
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	return b.String()
}

func Preflight(in Input) *Report {
	var findings []Finding

	seen := map[string]bool{}
	for _, rt := range in.Runtimes {
		if rt == "" || seen[rt] {
			continue
		}
		seen[rt] = true
		findings = append(findings, CheckRuntimeAvailable(in.Config, rt)...)
	}

	findings = append(findings, CheckPromptBudget(in.Config, in.Spec)...)
	findings = append(findings, CheckAcceptanceCommands(in.Spec)...)
	findings = append(findings, CheckLockConflicts(in.Config, in.Spec)...)

	if in.Mode == "pipeline" {
		findings = append(findings, CheckActivePhases(in.Complexity)...)
	}

	return &Report{Findings: findings}
}
