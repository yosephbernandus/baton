package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yosephbernandus/baton/internal/config"
	"gopkg.in/yaml.v3"
)

// parseGenerated loads a generated config the way baton does, so a scaffold that
// baton cannot read fails here rather than on the user's first command.
func parseGenerated(t *testing.T, yamlText string) *config.Config {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(path, []byte(yamlText), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("generated config does not parse: %v\n\n%s", err, yamlText)
	}
	return &cfg
}

var detected = []detectedRuntime{
	{Name: "claude-code", Command: "claude"},
	{Name: "opencode", Command: "opencode"},
}

// `baton setup` wrote role_models entries as bare strings while the field wanted
// a mapping, so every scaffolded project failed to load its own config on the
// very next command. Nothing checked that the generator's output was readable.
func TestNonInteractiveConfigParses(t *testing.T) {
	cfg := parseGenerated(t, generateAgentsYAML(detected))

	if len(cfg.Runtimes) == 0 {
		t.Fatal("no runtimes in the generated config")
	}
	if len(cfg.RoleModels) == 0 {
		t.Fatal("no role_models in the generated config")
	}
}

func TestInteractiveConfigParses(t *testing.T) {
	cfg := parseGenerated(t, generateAgentsYAMLInteractive(
		detected, detected[0], "sonnet", detected[1], "kimi"))

	if len(cfg.RoleModels) == 0 {
		t.Fatal("no role_models in the generated config")
	}
	if rm := cfg.RoleModels["developer"]; rm.Runtime != "opencode" || rm.Model != "kimi" {
		t.Errorf("developer=%+v, want the chosen worker runtime and model", rm)
	}
}

// Every generated role entry must carry both a runtime and a model.
// resolveRoleRuntime requires both, so an entry missing either parses fine and
// is then silently ignored — falling back to exactly the defaults it was written
// to override.
func TestGeneratedRoleEntriesAreUsable(t *testing.T) {
	for name, yamlText := range map[string]string{
		"non-interactive": generateAgentsYAML(detected),
		"interactive": generateAgentsYAMLInteractive(
			detected, detected[0], "sonnet", detected[1], "kimi"),
	} {
		cfg := parseGenerated(t, yamlText)
		for role, rm := range cfg.RoleModels {
			if rm.Runtime == "" {
				t.Errorf("%s: role %q has no runtime", name, role)
			}
			if rm.Model == "" {
				t.Errorf("%s: role %q has no model, so resolveRoleRuntime would ignore it", name, role)
			}
			if _, ok := cfg.Runtimes[rm.Runtime]; !ok {
				t.Errorf("%s: role %q names runtime %q, which the same file does not define",
					name, role, rm.Runtime)
			}
		}
	}
}

// Both generators cover the same roles, so switching between them does not
// silently drop one.
func TestBothGeneratorsCoverTheSameRoles(t *testing.T) {
	a := parseGenerated(t, generateAgentsYAML(detected))
	b := parseGenerated(t, generateAgentsYAMLInteractive(
		detected, detected[0], "sonnet", detected[1], "kimi"))

	for role := range a.RoleModels {
		if _, ok := b.RoleModels[role]; !ok {
			t.Errorf("role %q present non-interactively, missing interactively", role)
		}
	}
	for role := range b.RoleModels {
		if _, ok := a.RoleModels[role]; !ok {
			t.Errorf("role %q present interactively, missing non-interactively", role)
		}
	}
}

// A single runtime means no role split to write, and the config still has to
// load.
func TestConfigParsesWithOneRuntime(t *testing.T) {
	cfg := parseGenerated(t, generateAgentsYAML([]detectedRuntime{
		{Name: "opencode", Command: "opencode"},
	}))
	if len(cfg.Runtimes) != 1 {
		t.Errorf("runtimes=%v, want exactly one", cfg.Runtimes)
	}
}
