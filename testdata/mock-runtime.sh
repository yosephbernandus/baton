#!/bin/bash
# Mock runtime for testing. Echoes the prompt it receives.
while [[ $# -gt 0 ]]; do
  case "$1" in
    -m) MODEL="$2"; shift 2 ;;
    -p) PROMPT="$2"; shift 2 ;;
    --files) FILES="$2"; shift 2 ;;
    *) shift ;;
  esac
done

echo "mock-runtime received prompt"
echo "model: $MODEL"
echo "done"
exit 0
