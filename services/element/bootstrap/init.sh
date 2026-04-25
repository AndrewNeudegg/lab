#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
service_dir="$(cd "${script_dir}/.." && pwd)"
repo_root="$(cd "${service_dir}/../.." && pwd)"
env_file="${ENV_FILE:-${service_dir}/.env}"

random_hex() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
  fi
}

upsert_env_value() {
  local file="$1"
  local key="$2"
  local value="$3"

  if grep -q "^${key}=" "${file}" 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${value}|" "${file}"
  else
    printf '%s=%s\n' "${key}" "${value}" >>"${file}"
  fi
}

if [ ! -f "${env_file}" ]; then
  postgres_password="$(random_hex)"
  cat >"${env_file}" <<EOF
COMPOSE_PROJECT_NAME=element
ELEMENT_DATA_DIR=../../data/element

MATRIX_SERVER_NAME=localhost
SYNAPSE_REPORT_STATS=no
SYNAPSE_PORT=8008

ELEMENT_PORT=8080
ELEMENT_WEB_PORT=8080
ELEMENT_HOMESERVER_URL=http://lab:8008

POSTGRES_DB=synapse
POSTGRES_USER=synapse
POSTGRES_PASSWORD=${postgres_password}

ELEMENT_BOT_USERNAME=element-bot
ELEMENT_BOT_PASSWORD=$(random_hex)

PUID=$(id -u)
PGID=$(id -g)
TZ=UTC
EOF
fi

set -a
# shellcheck disable=SC1090
. "${env_file}"
set +a

ELEMENT_BOT_USERNAME="${ELEMENT_BOT_USERNAME:-element-bot}"
if [ -z "${ELEMENT_BOT_PASSWORD:-}" ]; then
  ELEMENT_BOT_PASSWORD="$(random_hex)"
fi

upsert_env_value "${env_file}" "ELEMENT_BOT_USERNAME" "${ELEMENT_BOT_USERNAME}"
upsert_env_value "${env_file}" "ELEMENT_BOT_PASSWORD" "${ELEMENT_BOT_PASSWORD}"

home_env="${HOME}/.env"
home_env_tmp="$(mktemp)"
if [ -f "${home_env}" ]; then
  awk '
    $0 == "# BEGIN element bot credentials" {skip=1; next}
    $0 == "# END element bot credentials" {skip=0; next}
    !skip {print}
  ' "${home_env}" >"${home_env_tmp}"
fi
{
  printf '%s\n' "# BEGIN element bot credentials"
  printf 'ELEMENT_BOT_USERNAME=%s\n' "${ELEMENT_BOT_USERNAME}"
  printf 'ELEMENT_BOT_PASSWORD=%s\n' "${ELEMENT_BOT_PASSWORD}"
  printf '%s\n' "# END element bot credentials"
} >>"${home_env_tmp}"
mv "${home_env_tmp}" "${home_env}"

case "${ELEMENT_DATA_DIR}" in
  /*) data_dir="${ELEMENT_DATA_DIR}" ;;
  *) data_dir="${service_dir}/${ELEMENT_DATA_DIR#./}" ;;
esac
synapse_data="${data_dir}/synapse"
element_data="${data_dir}/element-web"
postgres_data="${data_dir}/postgres"

mkdir -p "${synapse_data}" "${element_data}" "${postgres_data}"

cat >"${element_data}/config.json" <<EOF
{
  "default_server_config": {
    "m.homeserver": {
      "base_url": "${ELEMENT_HOMESERVER_URL}",
      "server_name": "${MATRIX_SERVER_NAME}"
    }
  },
  "brand": "Element",
  "disable_custom_urls": false,
  "disable_guests": true,
  "default_theme": "light"
}
EOF

cat >"${synapse_data}/local.yaml" <<EOF
database:
  name: psycopg2
  args:
    user: "${POSTGRES_USER}"
    password: "${POSTGRES_PASSWORD}"
    database: "${POSTGRES_DB}"
    host: "postgres"
    cp_min: 5
    cp_max: 10

enable_registration: true
enable_registration_without_verification: true
EOF

if [ ! -f "${synapse_data}/homeserver.yaml" ]; then
  podman run --rm \
    -v "${synapse_data}:/data:Z" \
    -e SYNAPSE_SERVER_NAME="${MATRIX_SERVER_NAME}" \
    -e SYNAPSE_REPORT_STATS="${SYNAPSE_REPORT_STATS}" \
    -e UID="${PUID}" \
    -e GID="${PGID}" \
    docker.io/matrixdotorg/synapse:latest generate
fi

printf 'Element data initialized in %s\n' "${data_dir}"
