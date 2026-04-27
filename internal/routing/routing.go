package routing

import (
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/spec"
)

type Resolution struct {
	Action  string
	Runtime string
	Model   string
	Reason  string
}

func Resolve(cfg *config.Config, s *spec.Spec) *Resolution {
	if s == nil || len(cfg.Routing.Rules) == 0 {
		return defaultResolution(cfg)
	}

	for _, rule := range cfg.Routing.Rules {
		if matches(rule, s) {
			return &Resolution{
				Action:  rule.Action,
				Runtime: resolveField(rule.Runtime, cfg.Defaults.Runtime),
				Model:   resolveField(rule.Model, cfg.Defaults.Model),
				Reason:  rule.Reason,
			}
		}
	}

	return defaultResolution(cfg)
}

func matches(rule config.RoutingRule, s *spec.Spec) bool {
	for key, val := range rule.Match {
		switch key {
		case "default":
			if b, ok := val.(bool); ok && b {
				return true
			}
		case "criticality", "estimated_complexity":
			expected, ok := val.(string)
			if !ok {
				continue
			}
			if key == "criticality" && s.Criticality != expected {
				return false
			}
		case "domain":
			if !matchDomain(val, s) {
				return false
			}
		}
	}
	return true
}

func matchDomain(val interface{}, s *spec.Spec) bool {
	domains, ok := toStringSlice(val)
	if !ok || len(domains) == 0 {
		return false
	}

	specDomains := extractDomains(s)
	if len(specDomains) == 0 {
		return false
	}

	for _, sd := range specDomains {
		for _, d := range domains {
			if sd == d {
				return true
			}
		}
	}
	return false
}

func extractDomains(s *spec.Spec) []string {
	var domains []string
	for _, cf := range s.ContextFiles {
		domains = append(domains, inferDomain(cf))
	}
	return domains
}

func inferDomain(path string) string {
	domainMap := map[string]string{
		".tsx":        "frontend",
		".jsx":        "frontend",
		".css":        "css",
		".scss":       "css",
		".vue":        "frontend",
		".svelte":     "frontend",
		"_test.go":    "tests",
		"_test.py":    "tests",
		".test.ts":    "tests",
		".test.tsx":   "tests",
		".spec.ts":    "tests",
		"terraform":   "infra",
		".tf":         "infra",
		"k8s":         "k8s",
		"kubernetes":  "k8s",
		"Dockerfile":  "infra",
		"docker-compose": "infra",
		".github":     "ci-cd",
		"ci":          "ci-cd",
		".yml":        "ci-cd",
	}

	for suffix, domain := range domainMap {
		if len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix {
			return domain
		}
	}

	for substr, domain := range domainMap {
		for i := 0; i <= len(path)-len(substr); i++ {
			if path[i:i+len(substr)] == substr {
				return domain
			}
		}
	}

	return "general"
}

func toStringSlice(val interface{}) ([]string, bool) {
	switch v := val.(type) {
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result, true
	case []string:
		return v, true
	case string:
		return []string{v}, true
	}
	return nil, false
}

func resolveField(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func defaultResolution(cfg *config.Config) *Resolution {
	return &Resolution{
		Action:  "delegate",
		Runtime: cfg.Defaults.Runtime,
		Model:   cfg.Defaults.Model,
		Reason:  "no matching routing rule",
	}
}
