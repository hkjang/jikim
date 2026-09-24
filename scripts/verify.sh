#!/usr/bin/env bash
set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly VERSION="$(${SCRIPT_DIR}/version.sh)"

RUN_DOCKER=false
RUN_SMOKE=false
for arg in "$@"; do
  case "${arg}" in
    --docker) RUN_DOCKER=true ;;
    --smoke) RUN_DOCKER=true; RUN_SMOKE=true ;;
    *) printf '사용법: %s [--docker] [--smoke]\n' "$0" >&2; exit 2 ;;
  esac
done

cd "${PROJECT_DIR}"

for required in go.mod cmd/server web/package.json web/package-lock.json Dockerfile docker-compose.yml; do
  [[ -e "${required}" ]] || { printf '필수 파일이 없습니다: %s\n' "${required}" >&2; exit 1; }
done

for script in scripts/*.sh; do
  bash -n "${script}"
done

mapfile -t GO_FILES < <(find . -type f -name '*.go' -not -path './.git/*' -not -path './web/node_modules/*' | sort)
if ((${#GO_FILES[@]} > 0)); then
  mapfile -t UNFORMATTED < <(gofmt -l "${GO_FILES[@]}")
  if ((${#UNFORMATTED[@]} > 0)); then
    printf 'gofmt가 필요한 파일:\n%s\n' "${UNFORMATTED[*]}" >&2
    exit 1
  fi
fi

mapfile -t GO_PACKAGES < <(go list ./... | grep -v '/web/')
if ((${#GO_PACKAGES[@]} == 0)); then
  printf '검증할 Go 패키지를 찾지 못했습니다.\n' >&2
  exit 1
fi
go test "${GO_PACKAGES[@]}"
go vet "${GO_PACKAGES[@]}"

# 성공 경로의 진행 출력(설치 목록·테스트 이름·산출물 목록)은 줄이고, 실패 상세는
# 각 도구가 그대로 내도록 둔다. 실패까지 숨기는 >/dev/null 리다이렉트는 쓰지 않는다.
npm --prefix web ci --no-audit --no-fund --loglevel=error
npm --prefix web test -- --maxWorkers=1 --reporter=dot
npm --prefix web run lint
VITE_APP_VERSION="${VERSION}" npm --prefix web run build -- --logLevel=warn
test -f web/dist/index.html
node scripts/verify-docs.mjs

POSTGRES_DSN='postgres://user:pass@db:5432/jikim?sslmode=disable' \
BOOTSTRAP_ADMIN='admin' \
BOOTSTRAP_ADMIN_PASSWORD='verification-only' \
ENCRYPTION_KEY='0123456789abcdef0123456789abcdef' \
docker compose config --quiet

if [[ "${RUN_DOCKER}" == true ]]; then
  "${SCRIPT_DIR}/build-image.sh" "jikim:${VERSION}"
fi

if [[ "${RUN_SMOKE}" == true ]]; then
  "${SCRIPT_DIR}/smoke-offline.sh" "jikim:${VERSION}"
fi

printf '검증 완료: jikim %s\n' "${VERSION}"
