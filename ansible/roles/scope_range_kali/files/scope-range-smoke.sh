#!/usr/bin/env bash
set -euo pipefail

config=/home/kali/.config/scope-range/services.env
set -a
source "$config"
set +a

test "$(dig +short web01.range.test | tail -n 1)" = "10.50.10.20"
curl --fail --silent --show-error "$WORDPRESS_URL/" >/dev/null
curl --fail --silent --show-error "$NEXTCLOUD_URL/status.php" >/dev/null
curl --fail --silent --show-error "$GITEA_URL/api/healthz" >/dev/null
curl --fail --silent --show-error \
  --user "alice:$FILES_PASSWORD" "$GITEA_URL/api/v1/user" \
  | jq --exit-status '.login == "alice"' >/dev/null
curl --fail --silent --show-error "$REGISTRY_URL/v2/" >/dev/null
PGPASSWORD="$POSTGRES_PASSWORD" psql \
  --host "$POSTGRES_HOST" --username "$POSTGRES_USER" --dbname business \
  --tuples-only --command 'SELECT count(*) FROM customers' | grep -Eq '[[:space:]]*2'
mysql \
  --host "$MARIADB_HOST" --user "$MARIADB_USER" \
  --password="$MARIADB_PASSWORD" wordpress \
  --batch --skip-column-names --execute 'SELECT count(*) FROM range_notes' | grep -qx '2'
redis-cli -u "$REDIS_URL" GET session:alice | grep -qx active
ldapsearch -LLL -x -H "$LDAP_URL" \
  -D "$LDAP_BIND_DN" -w "$LDAP_BIND_PASSWORD" \
  -b ou=people,dc=range,dc=test '(uid=alice)' uid | grep -qx 'uid: alice'
AWS_EC2_METADATA_DISABLED=true aws --endpoint-url "$S3_ENDPOINT" s3 ls >/dev/null
smbclient //files.range.test/shared -N -c ls >/dev/null
showmount --exports files.range.test | grep -q '/srv/range/shared'
