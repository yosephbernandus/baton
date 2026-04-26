package brief

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.md")
	os.WriteFile(path, []byte("Project: Test\nLanguage: Go"), 0o644)

	result := Load(path)
	if result != "Project: Test\nLanguage: Go" {
		t.Errorf("unexpected brief content: %q", result)
	}
}

func TestLoad_Missing(t *testing.T) {
	result := Load("/nonexistent/brief.md")
	if result != "" {
		t.Errorf("expected empty string for missing file, got %q", result)
	}
}
