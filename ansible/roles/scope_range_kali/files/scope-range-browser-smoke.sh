#!/usr/bin/env bash
set -euo pipefail

readonly CHROMIUM="$(command -v chromium || command -v chromium-browser)"
readonly WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

render() {
  local url="$1"
  local output="$2"
  timeout 60 "$CHROMIUM" \
    --headless \
    --no-sandbox \
    --disable-gpu \
    --disable-dev-shm-usage \
    --user-data-dir="$WORKDIR/profile" \
    --dump-dom "$url" >"$output" 2>/dev/null
}

render http://wordpress.range.test "$WORKDIR/wordpress.html"
grep -Fq 'Project KRAKEN Breeding Update' "$WORKDIR/wordpress.html"

render http://git.range.test:3000/poseidon/kraken-control-plane "$WORKDIR/gitea.html"
grep -Fq 'kraken-control-plane' "$WORKDIR/gitea.html"
