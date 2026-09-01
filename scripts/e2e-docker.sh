#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly VERSION="$(${SCRIPT_DIR}/version.sh)"
readonly IMAGE="${1:-jikim:${VERSION}}"
readonly POSTGRES_IMAGE="postgres:17-alpine"
readonly RUN_ID="${$}"
readonly NETWORK_NAME="jikim-e2e-${RUN_ID}"
readonly DB_NAME="jikim-e2e-db-${RUN_ID}"
readonly APP_NAME="jikim-e2e-app-${RUN_ID}"
readonly RELAY_NAME="jikim-e2e-relay-${RUN_ID}"
readonly DB_PASSWORD="jikim-e2e-db-only"
readonly ADMIN_USER="e2e-admin"
readonly ADMIN_PASSWORD="Jikim-E2E-Only-2026!"
readonly ENCRYPTION_KEY_VALUE="0123456789abcdef0123456789abcdef"
readonly PORT="${E2E_PORT:-18080}"

cleanup() {
  docker container rm --force "${RELAY_NAME}" >/dev/null 2>&1 || true
  docker container rm --force "${APP_NAME}" >/dev/null 2>&1 || true
  docker container rm --force "${DB_NAME}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

command -v docker >/dev/null 2>&1 || { printf 'docker 명령이 필요합니다.\n' >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { printf 'npm 명령이 필요합니다.\n' >&2; exit 1; }
docker image inspect "${IMAGE}" >/dev/null 2>&1 || { printf '이미지가 없습니다: %s\n' "${IMAGE}" >&2; exit 1; }
docker image inspect "${POSTGRES_IMAGE}" >/dev/null 2>&1 || {
  printf 'E2E 전에 이미지를 준비하세요: docker pull %s\n' "${POSTGRES_IMAGE}" >&2
  exit 1
}

docker network create --internal "${NETWORK_NAME}" >/dev/null
docker run --detach \
  --name "${DB_NAME}" \
  --network "${NETWORK_NAME}" \
  --network-alias postgres \
  --env POSTGRES_DB=jikim \
  --env POSTGRES_USER=jikim \
  --env "POSTGRES_PASSWORD=${DB_PASSWORD}" \
  "${POSTGRES_IMAGE}" >/dev/null

for _ in $(seq 1 30); do
  if docker exec "${DB_NAME}" pg_isready --host 127.0.0.1 --username jikim --dbname jikim >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "${DB_NAME}" pg_isready --host 127.0.0.1 --username jikim --dbname jikim >/dev/null

docker run --detach \
  --name "${APP_NAME}" \
  --network "${NETWORK_NAME}" \
  --network-alias jikim-app \
  --read-only \
  --tmpfs /tmp:size=64m,mode=1777 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --env "POSTGRES_DSN=postgres://jikim:${DB_PASSWORD}@postgres:5432/jikim?sslmode=disable" \
  --env "BOOTSTRAP_ADMIN=${ADMIN_USER}" \
  --env "BOOTSTRAP_ADMIN_PASSWORD=${ADMIN_PASSWORD}" \
  --env "ENCRYPTION_KEY=${ENCRYPTION_KEY_VALUE}" \
  "${IMAGE}" >/dev/null

# Docker의 internal bridge는 앱의 외부 통신을 차단하지만 일부 runner에서는
# publish한 포트의 host 접근도 함께 막는다. 앱은 internal network에만 유지하고,
# 별도 무권한 TCP relay만 host bridge와 internal network를 연결한다.
docker run --detach \
  --name "${RELAY_NAME}" \
  --network bridge \
  --publish "127.0.0.1:${PORT}:8080" \
  --read-only \
  --tmpfs /tmp:rw,exec,nosuid,nodev,size=1m,mode=1777 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --entrypoint /bin/sh \
  "${IMAGE}" -c \
  'printf "#!/bin/sh\nexec nc jikim-app 8080\n" > /tmp/relay && chmod 700 /tmp/relay && exec nc -lk -p 8080 -e /tmp/relay' \
  >/dev/null
docker network connect "${NETWORK_NAME}" "${RELAY_NAME}"

ready=false
for _ in $(seq 1 60); do
  if curl --fail --silent --show-error --max-time 3 "http://127.0.0.1:${PORT}/readyz" >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "${ready}" != true ]]; then
  docker logs "${APP_NAME}" >&2
  printf 'E2E 서비스 readiness 확인에 실패했습니다.\n' >&2
  exit 1
fi

cd "${PROJECT_DIR}/web"
E2E_BASE_URL="http://127.0.0.1:${PORT}" \
E2E_ADMIN="${ADMIN_USER}" \
E2E_ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
E2E_VERSION="${VERSION}" \
npm exec -- playwright test

printf '브라우저 E2E 완료: %s\n' "${IMAGE}"
