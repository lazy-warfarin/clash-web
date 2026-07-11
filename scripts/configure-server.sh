#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${CLASH_WEB_URL:-http://127.0.0.1:8080}"
SUBSCRIPTION_URL="${SUBSCRIPTION_URL:?SUBSCRIPTION_URL is required}"
PASSWORD_FILE="${CLASH_WEB_PASSWORD_FILE:-/root/clash-web-admin-password}"
BOOTSTRAP_FILE="${CLASH_WEB_BOOTSTRAP_FILE:-/var/lib/clash-web/bootstrap-password}"
COOKIE_JAR="$(mktemp)"
PROFILE_RESPONSE="$(mktemp)"
API_RESPONSE="$(mktemp)"
trap 'rm -f "$COOKIE_JAR" "$PROFILE_RESPONSE" "$API_RESPONSE"' EXIT

if [[ -s "$PASSWORD_FILE" ]]; then
  initial_password="$(tr -d '\r\n' < "$PASSWORD_FILE")"
  new_password="$initial_password"
  change_password=false
else
  initial_password="$(tr -d '\r\n' < "$BOOTSTRAP_FILE")"
  new_password="Cw-$(openssl rand -hex 18)"
  change_password=true
  umask 077
  printf '%s\n' "$new_password" > "$PASSWORD_FILE"
fi

login_payload="$(INITIAL_PASSWORD="$initial_password" python3 - <<'PY'
import json, os
print(json.dumps({"username": "admin", "password": os.environ["INITIAL_PASSWORD"]}))
PY
)"
curl --fail-with-body --silent --show-error \
  --cookie-jar "$COOKIE_JAR" \
  --header 'Content-Type: application/json' \
  --data "$login_payload" \
  "$BASE_URL/api/v1/auth/login" >/dev/null
echo 'Authenticated with Clash Web'

csrf="$(awk '$6 == "clash_web_csrf" { print $7 }' "$COOKIE_JAR")"
if [[ -z "$csrf" ]]; then
  echo 'login succeeded without a CSRF cookie' >&2
  exit 1
fi

if [[ "$change_password" == true ]]; then
  password_payload="$(INITIAL_PASSWORD="$initial_password" NEW_PASSWORD="$new_password" python3 - <<'PY'
import json, os
print(json.dumps({"current": os.environ["INITIAL_PASSWORD"], "password": os.environ["NEW_PASSWORD"]}))
PY
)"
  curl --fail-with-body --silent --show-error \
    --cookie "$COOKIE_JAR" \
    --header 'Content-Type: application/json' \
    --header "X-CSRF-Token: $csrf" \
    --data "$password_payload" \
    "$BASE_URL/api/v1/auth/password" >/dev/null
  echo 'Changed the bootstrap password'
fi

curl --fail-with-body --silent --show-error \
  --cookie "$COOKIE_JAR" \
  "$BASE_URL/api/v1/profiles/" > "$PROFILE_RESPONSE"
profile_id="$(python3 -c 'import json,sys; matches=[p for p in json.load(sys.stdin).get("profiles",[]) if p.get("name")=="YFJC"]; print(matches[0]["id"] if matches else "")' < "$PROFILE_RESPONSE")"

if [[ -z "$profile_id" ]]; then
  profile_payload="$(SUBSCRIPTION_URL="$SUBSCRIPTION_URL" python3 - <<'PY'
import json, os
print(json.dumps({
    "name": "YFJC",
    "source": "remote",
    "url": os.environ["SUBSCRIPTION_URL"],
}))
PY
)"
  curl --fail-with-body --silent --show-error \
    --cookie "$COOKIE_JAR" \
    --header 'Content-Type: application/json' \
    --header "X-CSRF-Token: $csrf" \
    --data "$profile_payload" \
    "$BASE_URL/api/v1/profiles/" > "$PROFILE_RESPONSE"

  profile_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' < "$PROFILE_RESPONSE")"
  echo "Created profile $profile_id"
else
  echo "Reusing profile $profile_id"
fi

if ! curl --fail-with-body --silent --show-error \
  --cookie "$COOKIE_JAR" \
  --header "X-CSRF-Token: $csrf" \
  --request POST \
  "$BASE_URL/api/v1/profiles/$profile_id/activate" > "$API_RESPONSE"; then
  cat "$API_RESPONSE" >&2
  exit 1
fi

printf 'Configured profile %s; administrator password stored in %s\n' "$profile_id" "$PASSWORD_FILE"
