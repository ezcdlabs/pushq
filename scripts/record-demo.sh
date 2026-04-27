#!/usr/bin/env bash
# Records the happy-path scenario as an animated GIF.
#
# Prerequisites:
#   pip install asciinema        (or: sudo apt install asciinema)
#   go install github.com/asciinema/agg@latest
#
# Usage:
#   ./scripts/record-demo.sh [scenario]
#
# Output: scripts/<scenario>.gif

set -euo pipefail

SCENARIO="${1:-happy-path}"
CAST="/tmp/${SCENARIO}.cast"
GIF="assets/${SCENARIO}.gif"

mkdir -p assets

go build -o /tmp/pushq-demo ./cmd/demo

asciinema rec --overwrite --cols 100 --rows 16 --command "/tmp/pushq-demo --play ${SCENARIO}" "$CAST"
agg "$CAST" "$GIF"
rm "$CAST"

echo "Written: $GIF"
