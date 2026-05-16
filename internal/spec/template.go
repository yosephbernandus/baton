package spec

import (
	"fmt"
	"regexp"
	"strings"
)

type TemplateOpts struct {
	Complexity  string
	Criticality string
	Domain      string
	WritesTo    []string
}

func GenerateTemplate(what string) string {
	return GenerateFromInputs(what, "", nil, nil, nil, TemplateOpts{})
}

func GenerateFromInputs(what, why string, constraints, contextFiles, criteria []string, opts TemplateOpts) string {
	var b strings.Builder

	b.WriteString("spec:\n")
	fmt.Fprintf(&b, "  what: |\n    %s\n\n", what)

	if why != "" {
		fmt.Fprintf(&b, "  why: |\n    %s\n\n", why)
	} else {
		b.WriteString("  why: |\n    TODO: Explain why this task exists and what depends on it.\n\n")
	}

	b.WriteString("  constraints:\n")
	if len(constraints) > 0 {
		for _, c := range constraints {
			fmt.Fprintf(&b, "    - \"%s\"\n", c)
		}
	} else {
		b.WriteString("    - \"TODO: What the worker must NOT do\"\n")
	}
	b.WriteString("\n")

	b.WriteString("  context_files:\n")
	if len(contextFiles) > 0 {
		for _, f := range contextFiles {
			fmt.Fprintf(&b, "    - \"%s\"\n", f)
		}
	} else {
		b.WriteString("    - \"TODO: Files the worker should read for context\"\n")
	}
	b.WriteString("\n")

	b.WriteString("  acceptance_criteria:\n")
	if len(criteria) > 0 {
		for _, ac := range criteria {
			fmt.Fprintf(&b, "    - \"%s\"\n", ac)
		}
	} else {
		b.WriteString("    - \"TODO: How to verify the work is correct\"\n")
	}
	b.WriteString("\n")

	if opts.Complexity != "" {
		fmt.Fprintf(&b, "  estimated_complexity: %s\n\n", opts.Complexity)
	}

	if opts.Criticality != "" {
		fmt.Fprintf(&b, "  criticality: %s\n\n", opts.Criticality)
	}

	if opts.Domain != "" {
		fmt.Fprintf(&b, "  domain: %s\n\n", opts.Domain)
	}

	if len(opts.WritesTo) > 0 {
		b.WriteString("  writes_to:\n")
		for _, w := range opts.WritesTo {
			fmt.Fprintf(&b, "    - \"%s\"\n", w)
		}
		b.WriteString("\n")
	}

	b.WriteString("  # acceptance_checks:\n")
	b.WriteString("  #   - command: \"go build ./...\"\n")
	b.WriteString("  #     expect_exit: 0\n")
	b.WriteString("  #     description: \"Code must compile\"\n")

	return b.String()
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.TrimRight(s, "-")
	}
	return s
}
