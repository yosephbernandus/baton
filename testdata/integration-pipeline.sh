#!/bin/bash
# Integration test for baton pipeline run with mock runtime.
# Tests TRIVIAL complexity: should run phases 1 (setup), 8 (implementation), 16 (completion).
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TMPDIR=$(mktemp -d)

trap 'rm -rf "$TMPDIR"' EXIT

# Build baton
cd "$PROJECT_DIR"
go build -o "$TMPDIR/baton" .

# Set up test project directory
mkdir -p "$TMPDIR/project/.baton"

# Copy main.go so context_files validation passes
cp "$PROJECT_DIR/main.go" "$TMPDIR/project/main.go"

# Write config with mock-pipeline runtime
cat > "$TMPDIR/project/.baton/agents.yaml" <<'EOF'
defaults:
  runtime: mock-pipeline
  model: test-model

runtimes:
  mock-pipeline:
    command: SCRIPT_DIR_PLACEHOLDER/mock-pipeline-runtime.sh
    prompt_flag: "-p"
    models:
      - test-model

task_dir: ".baton/tasks"
event_log: ".baton/events.ndjson"
default_timeout: "5m"
silence_timeout: "2m"
EOF

# Replace placeholder with actual path
sed "s|SCRIPT_DIR_PLACEHOLDER|$SCRIPT_DIR|g" "$TMPDIR/project/.baton/agents.yaml" > "$TMPDIR/project/.baton/agents.yaml.tmp"
mv "$TMPDIR/project/.baton/agents.yaml.tmp" "$TMPDIR/project/.baton/agents.yaml"

# Copy test spec
cp "$SCRIPT_DIR/pipeline-test-spec.yaml" "$TMPDIR/project/spec.yaml"

# Run pipeline
cd "$TMPDIR/project"
echo "=== Running pipeline (TRIVIAL) ==="
"$TMPDIR/baton" pipeline run spec.yaml --complexity TRIVIAL 2>&1 | tee "$TMPDIR/output.txt"
EXIT_CODE=${PIPESTATUS[0]}

echo ""
echo "=== Exit code: $EXIT_CODE ==="

# Verify output
if [ $EXIT_CODE -ne 0 ]; then
  echo "FAIL: expected exit code 0, got $EXIT_CODE"
  exit 1
fi

# Check that only 3 phases ran (TRIVIAL: setup, implementation, completion)
if grep -q "3 active" "$TMPDIR/output.txt"; then
  echo "PASS: 3 active phases for TRIVIAL"
else
  echo "FAIL: expected 3 active phases"
  grep "Phases:" "$TMPDIR/output.txt"
  exit 1
fi

if grep -q "13 skipped" "$TMPDIR/output.txt"; then
  echo "PASS: 13 skipped phases"
else
  echo "FAIL: expected 13 skipped phases"
  grep "Phases:" "$TMPDIR/output.txt"
  exit 1
fi

if grep -q "Status:.*completed" "$TMPDIR/output.txt"; then
  echo "PASS: pipeline completed"
else
  echo "FAIL: expected completed status"
  grep "Status:" "$TMPDIR/output.txt"
  exit 1
fi

echo ""
echo "=== All checks passed ==="
