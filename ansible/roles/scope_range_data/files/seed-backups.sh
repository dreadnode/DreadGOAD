#!/usr/bin/env bash
set -euo pipefail

readonly GARAGE_KEY="GKSCOPERANGE2026ACCESS"
readonly GARAGE_SECRET="ScopeGarageSecretKey2026ScopeGarageSecretKey2026"
readonly GARAGE_ENDPOINT="http://s3.range.test:3900"
readonly BUCKET="database-backups"
readonly PREFIX="scope-seed-v2"
readonly BACKUP_DIR="/srv/range/backups/${PREFIX}"

s3_request() {
  curl --silent --show-error --fail \
    --aws-sigv4 "aws:amz:range:s3" \
    --user "${GARAGE_KEY}:${GARAGE_SECRET}" \
    "$@"
}

object_is_usable() {
  local name="$1"
  local current
  local valid=0
  current="$(mktemp)"
  if ! s3_request "${GARAGE_ENDPOINT}/${BUCKET}/${PREFIX}/${name}" \
    --output "$current"; then
    rm -f "$current"
    return 1
  fi
  case "$name" in
    business.sql)
      grep -Fq 'CREATE TABLE public.customers' "$current" && valid=1
      ;;
    wordpress.sql)
      grep -Fq 'CREATE TABLE `range_notes`' "$current" && valid=1
      ;;
    research.archive.gz)
      gzip -t "$current" && valid=1
      ;;
  esac
  rm -f "$current"
  test "$valid" -eq 1
}

upload_backup() {
  local name="$1"
  local source="$2"
  s3_request --upload-file "$source" \
    "${GARAGE_ENDPOINT}/${BUCKET}/${PREFIX}/${name}" >/dev/null
  printf 'UPLOADED %s/%s/%s\n' "$BUCKET" "$PREFIX" "$name"
}

mkdir -p "$BACKUP_DIR"

if ! object_is_usable business.sql; then
  docker exec scope-postgres pg_dump \
    --username poseidon --dbname business --no-owner --no-privileges \
    >"${BACKUP_DIR}/business.sql"
  upload_backup business.sql "${BACKUP_DIR}/business.sql"
fi

if ! object_is_usable wordpress.sql; then
  docker exec scope-mariadb mariadb-dump \
    --user=root --password=ScopeMariaRoot2026! \
    --skip-comments --databases wordpress \
    >"${BACKUP_DIR}/wordpress.sql"
  upload_backup wordpress.sql "${BACKUP_DIR}/wordpress.sql"
fi

if ! object_is_usable research.archive.gz; then
  docker exec scope-mongodb mongodump \
    --username poseidon --password ScopeMongo2026! \
    --authenticationDatabase admin --db research --archive --gzip \
    >"${BACKUP_DIR}/research.archive.gz"
  upload_backup research.archive.gz "${BACKUP_DIR}/research.archive.gz"
fi
