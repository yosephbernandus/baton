#!/bin/bash
# Mock conversational worker using BATON: stdout markers protocol.
# Prints progress markers, gets stuck, waits for guidance, then completes.

# Parse flags (same pattern as other mock runtimes)
PROMPT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m) MODEL="$2"; shift 2 ;;
    -p) PROMPT="$2"; shift 2 ;;
    --files) FILES="$2"; shift 2 ;;
    *) PROMPT="${PROMPT:-$1}"; shift ;;
  esac
done

# Extract inbox path from prompt
INBOX=$(echo "$PROMPT" | grep -o 'Check .*/inbox.ndjson' | sed 's/Check //;s/ for.*//')
if [ -z "$INBOX" ]; then
  INBOX=$(echo "$PROMPT" | grep -o '[^ ]*/inbox.ndjson' | head -1)
fi

echo "mock-conversational starting"

# Phase 1: heartbeat
echo "BATON:H:starting work"
sleep 1

# Phase 2: progress
echo "BATON:P:20:reading codebase"
sleep 1

# Phase 3: stuck — ask question
echo "BATON:S:unsure which schema to use - v1 or v2?"

# Phase 4: wait for inbox guidance (max 20 seconds)
for i in $(seq 1 20); do
  if [ -n "$INBOX" ] && [ -s "$INBOX" ]; then
    GUIDANCE=$(tail -1 "$INBOX" | grep -o '"msg":"[^"]*"' | sed 's/"msg":"//;s/"//')
    echo "received guidance: $GUIDANCE"

    # Phase 5: continue with guidance
    echo "BATON:P:70:applying guidance"
    sleep 1

    echo "BATON:M:task complete"
    echo "done"
    exit 0
  fi
  sleep 1
done

# No guidance received — exit with clarification code
echo "CLARIFICATION_NEEDED: v1 or v2 schema?"
exit 10
