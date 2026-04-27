#!/bin/bash
while [[ $# -gt 0 ]]; do shift; done
echo "holding lock..."
sleep 10
echo "done"
