#!/bin/bash
# Mock runtime for pipeline testing.
# Parses the phase from the prompt and outputs the appropriate BATON:C completion marker.
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p) PROMPT="$2"; shift 2 ;;
    *) shift ;;
  esac
done

echo "BATON:H:working on phase"

# Extract phase name from prompt: "=== PHASE: setup (#1 of 16) ==="
PHASE_NAME=$(echo "$PROMPT" | grep -o 'PHASE: [a-z_]*' | head -1 | sed 's/PHASE: //')

if [ -z "$PHASE_NAME" ]; then
  echo "mock-runtime: could not find phase name in prompt"
  echo "BATON:C:unknown:fail:no phase found"
  exit 1
fi

echo "mock-runtime: phase=$PHASE_NAME"
echo "BATON:P:50:working on $PHASE_NAME"
echo "BATON:M:completed $PHASE_NAME"
echo "BATON:C:${PHASE_NAME}:done"
exit 0
