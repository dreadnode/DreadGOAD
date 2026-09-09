#!/usr/bin/env bash
set -euo pipefail

readonly CHROMIUM="$(command -v chromium || command -v chromium-browser)"
readonly WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

render_with_content() {
  local url="$1"
  local output="$2"
  local expected="$3"

  for attempt in 1 2 3; do
    if timeout 15 "$CHROMIUM" \
      --headless \
      --no-sandbox \
      --disable-gpu \
      --disable-dev-shm-usage \
      --user-data-dir="$WORKDIR/profile" \
      --dump-dom "$url" >"$output" 2>/dev/null \
      && grep -Fq "$expected" "$output"; then
      return 0
    fi
    if [[ "$attempt" -lt 3 ]]; then
      sleep 2
    fi
  done
  return 1
}

render_with_content \
  http://wordpress.range.test \
  "$WORKDIR/wordpress.html" \
  'Project KRAKEN Breeding Update'
render_with_content \
  http://git.range.test:3000/poseidon/kraken-control-plane \
  "$WORKDIR/gitea.html" \
  'kraken-control-plane'
