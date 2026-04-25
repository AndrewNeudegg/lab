#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
service_dir="$(cd "${script_dir}/.." && pwd)"

env_file="${service_dir}/.env"
if [ ! -f "${env_file}" ]; then
  echo "Missing ${env_file}; run bootstrap first." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "${env_file}"
set +a

: "${ELEMENT_BOT_USERNAME:=element-bot}"
: "${ELEMENT_BOT_PASSWORD:?missing ELEMENT_BOT_PASSWORD}"
: "${MATRIX_SERVER_NAME:=localhost}"

payload=$(printf '{"username":"%s","password":"%s","auth":{"type":"m.login.dummy"}}' \
  "${ELEMENT_BOT_USERNAME}" \
  "${ELEMENT_BOT_PASSWORD}")

response="$(
  curl -sS -w '\n%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST \
    -d "${payload}" \
    "http://127.0.0.1:8008/_matrix/client/v3/register"
)"

body="${response%$'\n'*}"
code="${response##*$'\n'}"

if [ "${code}" = "200" ]; then
  printf '%s\n' "${body}"
  exit 0
fi

if [ "${code}" = "400" ] && printf '%s' "${body}" | grep -q '"M_USER_IN_USE"'; then
  printf '%s\n' "${body}"
  exit 0
fi

printf '%s\n' "${body}" >&2
exit 1
