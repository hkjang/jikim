#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly VERSION="$(${SCRIPT_DIR}/version.sh)"
readonly IMAGE="${1:-jikim:${VERSION}}"
readonly POSTGRES_IMAGE="postgres:17-alpine"
readonly RUN_ID="${$}"
readonly NETWORK_NAME="jikim-smoke-${RUN_ID}"
readonly DB_NAME="jikim-smoke-db-${RUN_ID}"
readonly APP_NAME="jikim-smoke-app-${RUN_ID}"
readonly DB_PASSWORD="jikim-smoke-db-only"
readonly ADMIN_PASSWORD="Jikim-Smoke-Only-2026!"
readonly ENCRYPTION_KEY_VALUE="0123456789abcdef0123456789abcdef"

cleanup() {
  docker container rm --force "${APP_NAME}" >/dev/null 2>&1 || true
  docker container rm --force "${DB_NAME}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

command -v docker >/dev/null 2>&1 || { printf 'docker 명령이 필요합니다.\n' >&2; exit 1; }
docker image inspect "${IMAGE}" >/dev/null 2>&1 || { printf '이미지가 없습니다: %s\n' "${IMAGE}" >&2; exit 1; }
docker image inspect "${POSTGRES_IMAGE}" >/dev/null 2>&1 || {
  printf '스모크 테스트 전에 이미지를 준비하세요: docker pull %s\n' "${POSTGRES_IMAGE}" >&2
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
  # PostgreSQL 이미지의 초기화용 임시 서버(Unix socket)를 준비 완료로
  # 오인하지 않도록 애플리케이션과 같은 TCP 경로를 확인합니다.
  if docker exec "${DB_NAME}" pg_isready --host 127.0.0.1 --username jikim --dbname jikim >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "${DB_NAME}" pg_isready --host 127.0.0.1 --username jikim --dbname jikim >/dev/null

docker run --detach \
  --name "${APP_NAME}" \
  --network "${NETWORK_NAME}" \
  --read-only \
  --tmpfs /tmp:size=64m,mode=1777 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --env "POSTGRES_DSN=postgres://jikim:${DB_PASSWORD}@postgres:5432/jikim?sslmode=disable" \
  --env BOOTSTRAP_ADMIN=smoke-admin \
  --env "BOOTSTRAP_ADMIN_PASSWORD=${ADMIN_PASSWORD}" \
  --env "ENCRYPTION_KEY=${ENCRYPTION_KEY_VALUE}" \
  "${IMAGE}" >/dev/null

ready=false
for _ in $(seq 1 60); do
  if docker exec "${APP_NAME}" wget -q -T 3 -O - http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done

if [[ "${ready}" != true ]]; then
  docker logs "${APP_NAME}" >&2
  printf 'readiness 확인에 실패했습니다.\n' >&2
  exit 1
fi

docker exec "${APP_NAME}" wget -q -T 3 -O - http://127.0.0.1:8080/healthz >/dev/null
docker exec "${APP_NAME}" wget -q -T 3 -O - http://127.0.0.1:8080/v1/sys/health >/dev/null

version_response="$(docker exec "${APP_NAME}" wget -q -T 3 -O - http://127.0.0.1:8080/api/v1/version)"
if [[ "${version_response}" != *"\"version\":\"${VERSION}\""* ]]; then
  printf '백엔드 버전이 이미지 버전과 일치하지 않습니다: %s\n' "${version_response}" >&2
  exit 1
fi

if ! docker exec "${APP_NAME}" grep -R -F -q "${VERSION}" /app/web/assets; then
  printf '프런트엔드 번들에서 이미지 버전을 찾을 수 없습니다: %s\n' "${VERSION}" >&2
  exit 1
fi

if docker exec "${APP_NAME}" wget -q -T 2 -O /dev/null http://1.1.1.1 >/dev/null 2>&1; then
  printf '내부 네트워크에서 외부 통신이 가능하여 테스트를 중단합니다.\n' >&2
  exit 1
fi

printf '폐쇄망 스모크 테스트 완료: %s\n' "${IMAGE}"
