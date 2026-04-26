#!/bin/bash
# Mock runtime that hangs forever, for timeout testing.
while [[ $# -gt 0 ]]; do shift; done
echo "starting long task..."
sleep 300
