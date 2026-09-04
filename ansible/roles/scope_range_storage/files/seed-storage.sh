#!/usr/bin/env bash
set -euo pipefail

readonly GARAGE_CONTAINER="scope-garage"
readonly GARAGE_KEY="GKSCOPERANGE2026ACCESS"
readonly GARAGE_SECRET="ScopeGarageSecretKey2026ScopeGarageSecretKey2026"
readonly GARAGE_ENDPOINT="http://127.0.0.1:3900"
readonly SEED_ROOT="/opt/scope-range/storage/seed"

bucket_exists() {
  docker exec "$GARAGE_CONTAINER" /garage bucket info "$1" >/dev/null 2>&1
}

ensure_bucket() {
  local bucket="$1"
  if ! bucket_exists "$bucket"; then
    docker exec "$GARAGE_CONTAINER" /garage bucket create "$bucket" >/dev/null
    printf 'CREATED bucket %s\n' "$bucket"
  fi
  docker exec "$GARAGE_CONTAINER" /garage bucket allow \
    --read --write --owner --key "$GARAGE_KEY" "$bucket" >/dev/null
}

s3_request() {
  curl --silent --show-error --fail \
    --aws-sigv4 "aws:amz:range:s3" \
    --user "${GARAGE_KEY}:${GARAGE_SECRET}" \
    "$@"
}

ensure_object() {
  local bucket="$1"
  local key="$2"
  local source="$3"
  local current
  current="$(mktemp)"
  if s3_request "${GARAGE_ENDPOINT}/${bucket}/${key}" --output "$current" \
    && cmp --silent "$source" "$current"; then
    rm -f "$current"
    return
  fi
  rm -f "$current"
  s3_request --upload-file "$source" "${GARAGE_ENDPOINT}/${bucket}/${key}" >/dev/null
  printf 'UPLOADED %s/%s\n' "$bucket" "$key"
}

for bucket in range-assets build-artifacts database-backups research-archives; do
  ensure_bucket "$bucket"
done

ensure_object \
  range-assets orchid/brief.json \
  "$SEED_ROOT/range-assets/orchid/brief.json"
ensure_object \
  research-archives orchid/experiment-summary.json \
  "$SEED_ROOT/research-archives/orchid/experiment-summary.json"
