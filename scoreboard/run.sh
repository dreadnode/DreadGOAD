#!/usr/bin/env bash
# Run the DreadGOAD scoreboard from anywhere.
#
# Usage:
#   ./scoreboard/run.sh demo
#   ./scoreboard/run.sh generate-key
#   ./scoreboard/run.sh run --transport local --report /tmp/report.json
#   ./scoreboard/run.sh run --transport ssm --instance-id i-0abc123

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$REPO_ROOT"
exec python3 -m scoreboard "$@"
