package annealing

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yosephbernandus/baton/internal/feedback"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Enabled         bool   `yaml:"enabled"`
	AutoApply       bool   `yaml:"auto_apply"`
	AutoApplyMaxRisk string `yaml:"auto_apply_max_risk"`
	MinConfidence   string `yaml:"min_confidence"`
	PatchDir        string `yaml:"patch_dir"`
}

type Patch struct {
	ID            string      `yaml:"id"`
	Pattern       string      `yaml:"pattern"`
	Description   string      `yaml:"description"`
	Confidence    string      `yaml:"confidence"`
	Risk          string      `yaml:"risk"`
	TargetFile    string      `yaml:"target_file"`
	TargetPath    string      `yaml:"target_path"`
	CurrentValue  interface{} `yaml:"current_value,omitempty"`
	ProposedValue interface{} `yaml:"proposed_value"`
	Rationale     string      `yaml:"rationale"`
	Applied       bool        `yaml:"applied"`
	AppliedAt     *time.Time  `yaml:"applied_at,omitempty"`
}

type PatchFile struct {
	GeneratedAt time.Time `yaml:"generated_at"`
	BasedOn     string    `yaml:"based_on"`
	Patches     []Patch   `yaml:"patches"`
}

type Annealer struct {
	cfg      Config
	patchDir string
}

func New(cfg Config) *Annealer {
	patchDir := cfg.PatchDir
	if patchDir == "" {
		patchDir = ".baton/annealing"
	}
	return &Annealer{cfg: cfg, patchDir: patchDir}
}

func (a *Annealer) GeneratePatches(analysis *feedback.Analysis) (*PatchFile, error) {
	if analysis == nil {
		return nil, fmt.Errorf("nil analysis")
	}

	minConf := a.cfg.MinConfidence
	if minConf == "" {
		minConf = "medium"
	}

	var patches []Patch
	patchNum := 0

	for _, pattern := range analysis.Patterns {
		if !meetsConfidence(pattern.Confidence, minConf) {
			continue
		}

		patchNum++
		p := a.patternToPatch(pattern, patchNum)
		if p != nil {
			patches = append(patches, *p)
		}
	}

	pf := &PatchFile{
		GeneratedAt: time.Now().UTC(),
		BasedOn:     ".baton/feedback/analysis.yaml",
		Patches:     patches,
	}

	if err := a.savePatchFile(pf); err != nil {
		return nil, err
	}

	return pf, nil
}

func (a *Annealer) patternToPatch(p feedback.Pattern, num int) *Patch {
	id := fmt.Sprintf("patch-%03d", num)

	switch p.Type {
	case "runtime_domain_mismatch":
		return &Patch{
			ID:          id,
			Pattern:     p.Type,
			Description: p.Suggestion,
			Confidence:  p.Confidence,
			Risk:        "low",
			TargetFile:  ".baton/agents.yaml",
			TargetPath:  "routing.rules",
			ProposedValue: map[string]interface{}{
				"description": p.Description,
				"suggestion":  p.Suggestion,
			},
			Rationale: fmt.Sprintf("%s (%d occurrences)", p.Description, p.Occurrences),
		}

	case "retry_budget_insufficient":
		return &Patch{
			ID:            id,
			Pattern:       p.Type,
			Description:   p.Suggestion,
			Confidence:    p.Confidence,
			Risk:          "medium",
			TargetFile:    ".baton/agents.yaml",
			TargetPath:    "phase_machine.max_l1_retries",
			ProposedValue: 5,
			Rationale:     fmt.Sprintf("%s (%d occurrences)", p.Description, p.Occurrences),
		}

	case "retry_budget_excessive":
		return &Patch{
			ID:            id,
			Pattern:       p.Type,
			Description:   p.Suggestion,
			Confidence:    p.Confidence,
			Risk:          "low",
			TargetFile:    ".baton/agents.yaml",
			TargetPath:    "phase_machine.max_l1_retries",
			ProposedValue: 1,
			Rationale:     fmt.Sprintf("%s (%d occurrences)", p.Description, p.Occurrences),
		}

	case "loop_model_affinity":
		return &Patch{
			ID:          id,
			Pattern:     p.Type,
			Description: p.Suggestion,
			Confidence:  p.Confidence,
			Risk:        "medium",
			TargetFile:  ".baton/agents.yaml",
			TargetPath:  "role_models",
			ProposedValue: map[string]interface{}{
				"description": p.Description,
				"suggestion":  p.Suggestion,
			},
			Rationale: fmt.Sprintf("%s (%d occurrences)", p.Description, p.Occurrences),
		}

	case "timeout_mismatch":
		return &Patch{
			ID:            id,
			Pattern:       p.Type,
			Description:   p.Suggestion,
			Confidence:    p.Confidence,
			Risk:          "low",
			TargetFile:    ".baton/agents.yaml",
			TargetPath:    "default_timeout",
			ProposedValue: "120m",
			Rationale:     fmt.Sprintf("%s (%d occurrences)", p.Description, p.Occurrences),
		}

	default:
		return &Patch{
			ID:          id,
			Pattern:     p.Type,
			Description: p.Suggestion,
			Confidence:  p.Confidence,
			Risk:        "medium",
			TargetFile:  ".baton/agents.yaml",
			TargetPath:  "manual_review",
			ProposedValue: map[string]interface{}{
				"description": p.Description,
				"suggestion":  p.Suggestion,
			},
			Rationale: fmt.Sprintf("%s (%d occurrences)", p.Description, p.Occurrences),
		}
	}
}

func (a *Annealer) AutoApplyEligible(patches []Patch) []Patch {
	maxRisk := a.cfg.AutoApplyMaxRisk
	if maxRisk == "" {
		maxRisk = "low"
	}

	var eligible []Patch
	for _, p := range patches {
		if p.Applied {
			continue
		}
		if !meetsRiskThreshold(p.Risk, maxRisk) {
			continue
		}
		if isSafetyConfig(p.TargetPath) {
			continue
		}
		eligible = append(eligible, p)
	}
	return eligible
}

func (a *Annealer) LoadPatches() (*PatchFile, error) {
	path := filepath.Join(a.patchDir, "suggested-patches.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pf PatchFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

func (a *Annealer) savePatchFile(pf *PatchFile) error {
	if err := os.MkdirAll(a.patchDir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(pf)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.patchDir, "suggested-patches.yaml"), data, 0o644)
}

func meetsConfidence(actual, minimum string) bool {
	levels := map[string]int{"low": 1, "medium": 2, "high": 3}
	return levels[actual] >= levels[minimum]
}

func meetsRiskThreshold(actual, maxAllowed string) bool {
	levels := map[string]int{"low": 1, "medium": 2, "high": 3}
	return levels[actual] <= levels[maxAllowed]
}

func isSafetyConfig(targetPath string) bool {
	blocked := []string{
		"phase_machine.enabled",
		"escalation_advisor",
	}
	for _, b := range blocked {
		if strings.HasPrefix(targetPath, b) {
			return true
		}
	}
	return false
}
