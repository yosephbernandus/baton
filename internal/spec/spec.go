package spec

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Spec struct {
	What               string        `yaml:"what"`
	Why                string        `yaml:"why"`
	Constraints        []string      `yaml:"constraints"`
	ContextFiles       []string      `yaml:"context_files"`
	RelatedTasks       []RelatedTask `yaml:"related_tasks"`
	AcceptanceCriteria []string      `yaml:"acceptance_criteria"`
	Decisions          []Decision    `yaml:"decisions"`
	WritesTo           []string      `yaml:"writes_to"`
	Examples           []Example     `yaml:"examples"`
	AcceptanceChecks   []Check       `yaml:"acceptance_checks"`
	Criticality        string        `yaml:"criticality"`
}

type RelatedTask struct {
	TaskID         string `yaml:"task_id"`
	Status         string `yaml:"status"`
	Summary        string `yaml:"summary"`
	RelevantOutput string `yaml:"relevant_output"`
}

type Decision struct {
	Question  string `yaml:"question"`
	Answer    string `yaml:"answer"`
	Reason    string `yaml:"reason"`
	DecidedBy string `yaml:"decided_by"`
}

type Example struct {
	Description string `yaml:"description"`
	Code        string `yaml:"code"`
}

type Check struct {
	Command     string `yaml:"command"`
	ExpectExit  int    `yaml:"expect_exit"`
	Description string `yaml:"description"`
}

type specFile struct {
	Spec Spec `yaml:"spec"`
}

func Load(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	var sf specFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	return &sf.Spec, nil
}

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func Validate(s *Spec) []ValidationError {
	var errs []ValidationError

	if strings.TrimSpace(s.What) == "" {
		errs = append(errs, ValidationError{"what", "required, must be non-empty"})
	}
	if strings.TrimSpace(s.Why) == "" {
		errs = append(errs, ValidationError{"why", "required, must be non-empty"})
	}
	if s.Constraints == nil {
		errs = append(errs, ValidationError{"constraints", "required (can be empty array, but must be present)"})
	}
	if len(s.ContextFiles) == 0 {
		errs = append(errs, ValidationError{"context_files", "required, at least one file"})
	}
	for _, f := range s.ContextFiles {
		if _, err := os.Stat(f); err != nil {
			errs = append(errs, ValidationError{"context_files", fmt.Sprintf("file not found: %s", f)})
		}
	}
	if len(s.AcceptanceCriteria) == 0 {
		errs = append(errs, ValidationError{"acceptance_criteria", "required, at least one item"})
	}
	if s.Criticality != "" {
		switch s.Criticality {
		case "low", "medium", "high":
		default:
			errs = append(errs, ValidationError{"criticality", fmt.Sprintf("must be low|medium|high, got %q", s.Criticality)})
		}
	}
	for i, c := range s.AcceptanceChecks {
		if strings.TrimSpace(c.Command) == "" {
			errs = append(errs, ValidationError{"acceptance_checks", fmt.Sprintf("check[%d] has empty command", i)})
		}
	}

	return errs
}

func BuildPrompt(s *Spec, projectBrief string) string {
	var b strings.Builder

	if projectBrief != "" {
		b.WriteString("[PROJECT CONTEXT]\n")
		b.WriteString(projectBrief)
		b.WriteString("\n\n")
	}

	b.WriteString("[TASK]\n")
	b.WriteString(strings.TrimSpace(s.What))
	b.WriteString("\n\n")

	b.WriteString("[WHY THIS MATTERS]\n")
	b.WriteString(strings.TrimSpace(s.Why))
	b.WriteString("\n\n")

	if len(s.Constraints) > 0 {
		b.WriteString("[CONSTRAINTS]\n")
		for _, c := range s.Constraints {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(s.RelatedTasks) > 0 {
		b.WriteString("[RELATED TASKS — DO NOT CONFLICT]\n")
		for _, rt := range s.RelatedTasks {
			fmt.Fprintf(&b, "- %s (%s): %s\n", rt.TaskID, rt.Status, rt.Summary)
			if rt.RelevantOutput != "" {
				fmt.Fprintf(&b, "  Output: %s\n", rt.RelevantOutput)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("[ACCEPTANCE CRITERIA]\n")
	for _, ac := range s.AcceptanceCriteria {
		b.WriteString("- ")
		b.WriteString(ac)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(s.Decisions) > 0 {
		b.WriteString("[DECISIONS ALREADY MADE]\n")
		for _, d := range s.Decisions {
			fmt.Fprintf(&b, "- Q: %s -> A: %s (reason: %s, decided by: %s)\n", d.Question, d.Answer, d.Reason, d.DecidedBy)
		}
		b.WriteString("\n")
	}

	if len(s.Examples) > 0 {
		b.WriteString("[EXAMPLES]\n")
		for _, ex := range s.Examples {
			fmt.Fprintf(&b, "## %s\n%s\n", ex.Description, ex.Code)
		}
		b.WriteString("\n")
	}

	b.WriteString("[IMPORTANT]\n")
	b.WriteString("If you are uncertain about any aspect of this task, output the line:\n")
	b.WriteString("CLARIFICATION_NEEDED: <your question here>\n")
	b.WriteString("and exit. Do NOT guess.\n")

	return b.String()
}

func BuildPromptWithProtocol(basePrompt, taskID, taskDir string) string {
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString("\n[COMMUNICATION PROTOCOL]\n")
	b.WriteString("Report progress by printing these exact markers to stdout:\n\n")
	b.WriteString("  BATON:H:what you are doing          (heartbeat — every 60 seconds)\n")
	b.WriteString("  BATON:P:30:implementing auth        (progress — percent:description)\n")
	b.WriteString("  BATON:S:your specific question      (stuck — when blocked)\n")
	b.WriteString("  BATON:E:what failed                 (error — when something breaks)\n")
	b.WriteString("  BATON:M:what you completed          (milestone — subtask done)\n\n")
	b.WriteString("When stuck, print BATON:S:your question and wait up to 60 seconds.\n")
	b.WriteString("Check ")
	b.WriteString(taskDir)
	b.WriteString("/inbox.ndjson for guidance.\n")
	b.WriteString("If guidance arrives, follow it and continue working.\n")
	b.WriteString("If no guidance arrives within 60 seconds, exit with code 10.\n\n")
	b.WriteString("Do NOT exit with an error if you can ask for help first.\n")
	b.WriteString("Print BATON:S, wait for guidance, then proceed.\n")

	return b.String()
}

func BuildPromptWithResponse(s *Spec, projectBrief, clarification, answer, reason string) string {
	base := BuildPrompt(s, projectBrief)

	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n[PREVIOUS ATTEMPT]\n")
	if clarification != "" {
		fmt.Fprintf(&b, "The worker asked: %s\n", clarification)
	}
	fmt.Fprintf(&b, "Answer: %s\n", answer)
	if reason != "" {
		fmt.Fprintf(&b, "Reason: %s\n", reason)
	}
	b.WriteString("\nProceed with this guidance. Do NOT ask the same question again.\n")

	return b.String()
}
