package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Orchestrator    OrchestratorConfig       `yaml:"orchestrator"`
	Runtimes        map[string]RuntimeConfig `yaml:"runtimes"`
	Defaults        DefaultsConfig           `yaml:"defaults"`
	Routing         RoutingConfig            `yaml:"routing"`
	ClarifyPatterns []string                 `yaml:"clarification_patterns"`
	ClarifyExit     int                      `yaml:"clarification_exit_code"`
	EventLog        string                   `yaml:"event_log"`
	TaskDir         string                   `yaml:"task_dir"`
	ResultDir       string                   `yaml:"result_dir"`
	SpecDir         string                   `yaml:"spec_dir"`
	LockFile        string                   `yaml:"lock_file"`
	ProjectBrief    string                   `yaml:"project_brief"`
	LogMaxSizeMB    int                      `yaml:"log_max_size_mb"`
	LogKeepCount    int                      `yaml:"log_keep_count"`
	DefaultTimeout  string                   `yaml:"default_timeout"`
}

type OrchestratorConfig struct {
	Runtime string `yaml:"runtime"`
	Model   string `yaml:"model"`
}

type RuntimeConfig struct {
	Command     string   `yaml:"command"`
	ModelFlag   string   `yaml:"model_flag"`
	PromptFlag  string   `yaml:"prompt_flag"`
	ContextFlag string   `yaml:"context_flag"`
	ExtraFlags  []string `yaml:"extra_flags"`
	Workdir     string   `yaml:"workdir"`
	Models      []string `yaml:"models"`
}

type DefaultsConfig struct {
	Runtime string `yaml:"runtime"`
	Model   string `yaml:"model"`
}

type RoutingConfig struct {
	Rules              []RoutingRule `yaml:"rules"`
	CheckpointInterval int           `yaml:"checkpoint_interval"`
	CriticalReview     string        `yaml:"critical_review"`
}

type RoutingRule struct {
	Match  map[string]interface{} `yaml:"match"`
	Action string                 `yaml:"action"`
	Target string                 `yaml:"target,omitempty"`
	Runtime string                `yaml:"runtime,omitempty"`
	Model  string                 `yaml:"model,omitempty"`
	Reason string                 `yaml:"reason"`
}

func LoadConfig() (*Config, error) {
	cfg := defaultConfig()

	home, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(home, ".baton", "agents.yaml")
		if err := mergeFromFile(cfg, userPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading user config: %w", err)
		}
	}

	projectPath := filepath.Join(".baton", "agents.yaml")
	if err := mergeFromFile(cfg, projectPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading project config: %w", err)
	}

	return cfg, nil
}

func LoadConfigFromPath(path string) (*Config, error) {
	cfg := defaultConfig()
	if err := mergeFromFile(cfg, path); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) ValidateRuntime(name, model string) error {
	rt, ok := c.Runtimes[name]
	if !ok {
		available := make([]string, 0, len(c.Runtimes))
		for k := range c.Runtimes {
			available = append(available, k)
		}
		return fmt.Errorf("unknown runtime %q, available: %v", name, available)
	}

	for _, m := range rt.Models {
		if m == model {
			return nil
		}
	}
	return fmt.Errorf("model %q not available for runtime %q, available: %v", model, name, rt.Models)
}

func (c *Config) RuntimeAvailable(name string) bool {
	rt, ok := c.Runtimes[name]
	if !ok {
		return false
	}
	_, err := exec.LookPath(rt.Command)
	return err == nil
}

func (c *Config) ResolveRuntime(name, model string) (string, string) {
	if name == "" {
		name = c.Defaults.Runtime
	}
	if model == "" {
		model = c.Defaults.Model
	}
	return name, model
}

func defaultConfig() *Config {
	return &Config{
		ClarifyPatterns: []string{
			"I'm not sure",
			"unclear which",
			"could you clarify",
			"ambiguous",
			"multiple possible",
			"CLARIFICATION_NEEDED",
		},
		ClarifyExit:    10,
		EventLog:       ".baton/events.ndjson",
		TaskDir:        ".baton/tasks",
		ResultDir:      ".baton/results",
		SpecDir:        ".baton/specs",
		LockFile:       ".baton/locks.yaml",
		ProjectBrief:   ".baton/project-brief.md",
		LogMaxSizeMB:   10,
		LogKeepCount:   3,
		DefaultTimeout: "10m",
	}
}

func mergeFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}
