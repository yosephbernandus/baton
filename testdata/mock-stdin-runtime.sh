#!/bin/bash
# Mock runtime that reads prompt from stdin (simulates claude without -p).
# Outputs the prompt it received via stdin and any CLI args.

ARGS="$*"
PROMPT=$(cat)

echo "stdin-runtime received prompt via stdin"
echo "prompt: $PROMPT"
echo "args: $ARGS"
echo "BATON:H alive"
echo "BATON:P 50 halfway done"
echo "BATON:C implementation:done"
echo "done"
exit 0
