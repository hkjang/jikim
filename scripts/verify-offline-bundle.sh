#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly VERSION="$(${SCRIPT_DIR}/version.sh)"
readonly EXPECTED_TAG="jikim:${VERSION}"
readonly ARCHIVE="${1:-$(cd "${SCRIPT_DIR}/.." && pwd)/dist/jikim-${VERSION}.tar.gz}"
readonly CHECKSUM="${ARCHIVE}.sha256"

for tool in gzip tar jq sha256sum; do
  command -v "${tool}" >/dev/null 2>&1 || { printf '%s 명령이 필요합니다.\n' "${tool}" >&2; exit 1; }
done

[[ -f "${ARCHIVE}" ]] || { printf '번들이 없습니다: %s\n' "${ARCHIVE}" >&2; exit 1; }
[[ -f "${CHECKSUM}" ]] || { printf '체크섬이 없습니다: %s\n' "${CHECKSUM}" >&2; exit 1; }

(
  cd "$(dirname "${ARCHIVE}")"
  sha256sum --check "$(basename "${CHECKSUM}")"
)
gzip -t "${ARCHIVE}"

readonly REPOSITORY_TAGS="$(tar -xOzf "${ARCHIVE}" manifest.json | jq -r '.[].RepoTags[]?')"
if ! grep -Fxq "${EXPECTED_TAG}" <<<"${REPOSITORY_TAGS}"; then
  printf '번들에서 예상 이미지 태그를 찾지 못했습니다: %s\n발견한 태그:\n%s\n' \
    "${EXPECTED_TAG}" "${REPOSITORY_TAGS}" >&2
  exit 1
fi

printf '오프라인 번들 검증 완료: %s (%s)\n' "${ARCHIVE}" "${EXPECTED_TAG}"
