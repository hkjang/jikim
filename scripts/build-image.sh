#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly VERSION="$(${SCRIPT_DIR}/version.sh)"
readonly IMAGE="${1:-jikim:${VERSION}}"
readonly BUILD_COMMIT="$(git -C "${PROJECT_DIR}" rev-parse HEAD 2>/dev/null || printf unknown)"
readonly BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

command -v docker >/dev/null 2>&1 || { printf 'docker 명령이 필요합니다.\n' >&2; exit 1; }

# 레이어 로그는 파일에 받아 두고 실패했을 때만 그대로 보여 준다. --quiet는 실패한
# 레이어의 로그까지 지우므로 쓰지 않는다(smoke·e2e 스크립트의 docker logs 관례와 같다).
readonly BUILD_LOG="$(mktemp)"
trap 'rm -f "${BUILD_LOG}"' EXIT

cd "${PROJECT_DIR}"
if ! docker build \
  --build-arg "VERSION=${VERSION}" \
  --build-arg "COMMIT=${BUILD_COMMIT}" \
  --build-arg "BUILD_DATE=${BUILD_DATE}" \
  --tag "${IMAGE}" . > "${BUILD_LOG}" 2>&1; then
  cat "${BUILD_LOG}" >&2
  printf 'Docker 이미지 빌드에 실패했습니다: %s\n' "${IMAGE}" >&2
  exit 1
fi

printf 'Docker 이미지 빌드 완료: %s (%s)\n' "${IMAGE}" "${BUILD_COMMIT}"
