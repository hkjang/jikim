#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly VERSION="$(${SCRIPT_DIR}/version.sh)"
readonly IMAGE="jikim:${VERSION}"
readonly OUTPUT_DIR="${1:-${PROJECT_DIR}/dist}"
readonly ARCHIVE="${OUTPUT_DIR}/jikim-${VERSION}.tar.gz"
readonly CHECKSUM="${ARCHIVE}.sha256"

command -v docker >/dev/null 2>&1 || { printf 'docker 명령이 필요합니다.\n' >&2; exit 1; }
command -v gzip >/dev/null 2>&1 || { printf 'gzip 명령이 필요합니다.\n' >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { printf 'sha256sum 명령이 필요합니다.\n' >&2; exit 1; }

docker image inspect "${IMAGE}" >/dev/null 2>&1 || {
  printf '이미지를 찾을 수 없습니다: %s\n먼저 make docker를 실행하세요.\n' "${IMAGE}" >&2
  exit 1
}

readonly IMAGE_VERSION="$(docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.version" }}' "${IMAGE}")"
if [[ "${IMAGE_VERSION}" != "${VERSION}" ]]; then
  printf '이미지 레이블 버전(%s)과 릴리스 버전(%s)이 다릅니다.\n' "${IMAGE_VERSION}" "${VERSION}" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"
readonly TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEMP_DIR}"' EXIT

printf 'Docker 이미지를 저장하는 중: %s\n' "${IMAGE}"
docker image save "${IMAGE}" | gzip -n -9 > "${TEMP_DIR}/jikim.tar.gz"
gzip -t "${TEMP_DIR}/jikim.tar.gz"

mv "${TEMP_DIR}/jikim.tar.gz" "${ARCHIVE}"
(
  cd "${OUTPUT_DIR}"
  sha256sum "$(basename "${ARCHIVE}")" > "$(basename "${CHECKSUM}")"
)

printf '생성 완료:\n  %s\n  %s\n' "${ARCHIVE}" "${CHECKSUM}"
