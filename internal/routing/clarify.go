package routing

import (
	"strings"

	"github.com/yosephbernandus/baton/internal/decisions"
	"github.com/yosephbernandus/baton/internal/spec"
)

type ClarifyContext struct {
	Clarification string
	Spec          *spec.Spec
	ProjectBrief  string
	Decisions     []decisions.Record
}

type ClarifyVerdict struct {
	CanAutoAnswer bool
	Answer        string
	Reason        string
	Source        string
	Confidence    string
}

func AnalyzeClarification(ctx ClarifyContext) *ClarifyVerdict {
	if ctx.Clarification == "" {
		return &ClarifyVerdict{Confidence: "none"}
	}

	query := strings.ToLower(ctx.Clarification)

	if ctx.Spec != nil {
		for _, d := range ctx.Spec.Decisions {
			if containsAny(query, strings.ToLower(d.Question), strings.ToLower(d.Answer)) {
				return &ClarifyVerdict{
					CanAutoAnswer: true,
					Answer:        d.Answer,
					Reason:        d.Reason,
					Source:        "spec decisions",
					Confidence:    "high",
				}
			}
		}
	}

	for _, d := range ctx.Decisions {
		if containsAny(query, strings.ToLower(d.Question), strings.ToLower(d.Answer)) {
			return &ClarifyVerdict{
				CanAutoAnswer: true,
				Answer:        d.Answer,
				Reason:        d.Reason,
				Source:        "decisions.yaml (decided by " + d.DecidedBy + ")",
				Confidence:    "medium",
			}
		}
	}

	if ctx.ProjectBrief != "" {
		words := extractKeywords(query)
		briefLower := strings.ToLower(ctx.ProjectBrief)
		matches := 0
		for _, w := range words {
			if strings.Contains(briefLower, w) {
				matches++
			}
		}
		if len(words) > 0 && matches >= len(words)/2+1 {
			return &ClarifyVerdict{
				CanAutoAnswer: false,
				Source:        "project brief (partial match)",
				Confidence:    "low",
			}
		}
	}

	return &ClarifyVerdict{
		CanAutoAnswer: false,
		Confidence:    "none",
	}
}

func containsAny(query string, targets ...string) bool {
	words := extractKeywords(query)
	for _, target := range targets {
		matches := 0
		for _, w := range words {
			if strings.Contains(target, w) {
				matches++
			}
		}
		if len(words) > 0 && matches >= len(words)/2+1 {
			return true
		}
	}
	return false
}

func extractKeywords(s string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "shall": true, "can": true,
		"i": true, "you": true, "we": true, "they": true, "it": true,
		"this": true, "that": true, "which": true, "what": true, "who": true,
		"or": true, "and": true, "but": true, "if": true, "of": true,
		"to": true, "in": true, "for": true, "on": true, "with": true,
		"not": true, "no": true, "use": true, "using": true,
	}

	var keywords []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, ".,?!\"'()[]{}:;")
		if len(w) > 2 && !stopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}
