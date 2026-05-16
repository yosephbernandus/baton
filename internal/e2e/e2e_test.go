package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	batonBin  string
	buildOnce sync.Once
	buildErr  error
)

func buildBaton(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "baton-e2e-*")
		if err != nil {
			buildErr = fmt.Errorf("creating temp dir: %v", err)
			return
		}
		bin := filepath.Join(tmp, "baton")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		repoRoot := findRepoRoot(t)
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build failed: %v\n%s", err, out)
			return
		}
		batonBin = bin
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return batonBin
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod)")
		}
		dir = parent
	}
}

func setupProject(t *testing.T, bin string) string {
	t.Helper()
	dir := t.TempDir()
	repoRoot := findRepoRoot(t)
	mockRuntime := filepath.Join(repoRoot, "testdata", "mock-runtime.sh")
	mockDoctor := filepath.Join(repoRoot, "testdata", "mock-doctor-runtime.sh")

	agentsYAML := fmt.Sprintf(`orchestrator:
  runtime: mock
  model: test-model

runtimes:
  mock:
    command: "%s"
    prompt_flag: "-p"
    model_flag: "-m"
    models:
      - test-model
  mock-doctor:
    command: "%s"
    prompt_flag: "-p"
    models:
      - probe-model

defaults:
  runtime: mock
  model: test-model

task_dir: ".baton/tasks"
event_log: ".baton/events.ndjson"
`, mockRuntime, mockDoctor)

	batonDir := filepath.Join(dir, ".baton")
	if err := os.MkdirAll(filepath.Join(batonDir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(batonDir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(batonDir, "agents.yaml"), []byte(agentsYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func writeSpec(t *testing.T, dir string) string {
	t.Helper()
	contextFile := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(contextFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	specContent := fmt.Sprintf(`spec:
  what: "test task"
  why: "for e2e testing"
  constraints:
    - "none"
  context_files:
    - "%s"
  acceptance_criteria:
    - "test passes"
`, contextFile)

	specPath := filepath.Join(dir, ".baton", "specs", "test-task.yaml")
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return specPath
}

func runBaton(t *testing.T, bin, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec error: %v\n%s", err, out)
		}
	}
	return string(out), exitCode
}

func TestInitGeneratesValidCLAUDE_MD(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)
	specPath := writeSpec(t, dir)

	out, code := runBaton(t, bin, dir, "init", specPath, "--complexity", "MEDIUM")
	if code != 0 {
		t.Fatalf("init failed (exit %d): %s", code, out)
	}

	instrPath := filepath.Join(dir, ".baton", "tasks", "test-task", "AGENTS.md")
	data, err := os.ReadFile(instrPath)
	if err != nil {
		t.Fatalf("instructions file not found: %v", err)
	}

	content := string(data)
	sections := []string{
		"Baton Coordinator Protocol",
		"Task",
		"Active Phases",
		"Self-Phase Protocol",
		"Dispatch Protocol",
		"Retry Protocol",
		"L2 Loop Protocol",
		"Stuck Protocol",
		"Command Reference",
		"Phase Guidance",
		"Mandatory Phase Exit Questions",
	}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("instructions file missing section: %s", s)
		}
	}
}

func TestWorkerProtocolSequence(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)
	specPath := writeSpec(t, dir)

	out, code := runBaton(t, bin, dir, "init", specPath, "--complexity", "MEDIUM")
	if code != 0 {
		t.Fatalf("init failed (exit %d): %s", code, out)
	}

	taskID := "test-task"

	out, code = runBaton(t, bin, dir, "worker", "start", taskID)
	if code != 0 {
		t.Fatalf("worker start failed (exit %d): %s", code, out)
	}

	out, code = runBaton(t, bin, dir, "worker", "heartbeat", "working on it")
	if code != 0 {
		t.Fatalf("worker heartbeat failed (exit %d): %s", code, out)
	}

	out, code = runBaton(t, bin, dir, "worker", "progress", "50", "halfway done")
	if code != 0 {
		t.Fatalf("worker progress failed (exit %d): %s", code, out)
	}

	out, code = runBaton(t, bin, dir, "worker", "complete", taskID)
	if code != 0 {
		t.Fatalf("worker complete failed (exit %d): %s", code, out)
	}

	out, code = runBaton(t, bin, dir, "worker", "next", taskID)
	if code != 0 {
		t.Fatalf("worker next failed (exit %d): %s", code, out)
	}
	if !strings.Contains(strings.ToUpper(out), "PHASE") {
		t.Errorf("next should show phase info, got: %s", out)
	}
}

func TestWorkerRetryFlow(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)
	specPath := writeSpec(t, dir)

	_, code := runBaton(t, bin, dir, "init", specPath, "--complexity", "MEDIUM")
	if code != 0 {
		t.Fatal("init failed")
	}

	taskID := "test-task"

	runBaton(t, bin, dir, "worker", "start", taskID)

	out, code := runBaton(t, bin, dir, "worker", "fail", taskID, "compilation error")
	if code != 0 {
		t.Fatalf("worker fail failed (exit %d): %s", code, out)
	}

	out, code = runBaton(t, bin, dir, "worker", "retry", taskID)
	if code != 0 {
		t.Fatalf("worker retry failed (exit %d): %s", code, out)
	}
	if !strings.Contains(strings.ToUpper(out), "PHASE") {
		t.Errorf("retry should show phase info, got: %s", out)
	}

	out, code = runBaton(t, bin, dir, "worker", "status", taskID)
	if code != 0 {
		t.Fatalf("worker status failed (exit %d): %s", code, out)
	}
	if !strings.Contains(out, "L1") {
		t.Errorf("status should show L1 info, got: %s", out)
	}
}

func TestRunWithMockRuntime(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)
	specPath := writeSpec(t, dir)

	out, code := runBaton(t, bin, dir, "run",
		"--runtime", "mock",
		"--model", "test-model",
		"--spec", specPath,
		"--task-id", "e2e-run-test",
	)
	if code != 0 {
		t.Fatalf("run failed (exit %d): %s", code, out)
	}

	if !strings.Contains(out, "completed") && !strings.Contains(out, "done") {
		t.Logf("run output: %s", out)
	}
}

func TestRunFailsWithBadSpec(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)

	badSpec := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badSpec, []byte("spec:\n  what: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, code := runBaton(t, bin, dir, "run",
		"--runtime", "mock",
		"--model", "test-model",
		"--spec", badSpec,
	)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}
}

func TestDoctorCommand(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)

	out, code := runBaton(t, bin, dir, "doctor")
	if code != 0 {
		t.Fatalf("doctor failed (exit %d): %s", code, out)
	}
	if !strings.Contains(out, "mock") {
		t.Errorf("doctor should list mock runtime, got: %s", out)
	}
}

func TestPlanCommand(t *testing.T) {
	bin := buildBaton(t)
	dir := setupProject(t, bin)

	out, code := runBaton(t, bin, dir, "plan", "add JWT authentication")
	if code != 0 {
		t.Fatalf("plan failed (exit %d): %s", code, out)
	}

	specPath := filepath.Join(dir, ".baton", "specs", "add-jwt-authentication.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("spec not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "add JWT authentication") {
		t.Error("spec should contain the description")
	}
	if !strings.Contains(content, "TODO") {
		t.Error("non-interactive spec should have TODO placeholders")
	}
}
