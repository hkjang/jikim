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

npm --prefix web ci --no-audit --no-fund
npm --prefix web test -- --maxWorkers=1
npm --prefix web run lint
VITE_APP_VERSION="${VERSION}" npm --prefix web run build
test -f web/dist/index.html
node scripts/verify-docs.mjs

POSTGRES_DSN='postgres://user:pass@db:5432/jikim?sslmode=disable' \
BOOTSTRAP_ADMIN='admin' \
BOOTSTRAP_ADMIN_PASSWORD='verification-only' \
ENCRYPTION_KEY='0123456789abcdef0123456789abcdef' \
docker compose config --quiet

if [[ "${RUN_DOCKER}" == true ]]; then
  readonly BUILD_COMMIT="$(git rev-parse HEAD 2>/dev/null || printf unknown)"
  readonly BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  docker build \
    --build-arg "VERSION=${VERSION}" \
    --build-arg "COMMIT=${BUILD_COMMIT}" \
    --build-arg "BUILD_DATE=${BUILD_DATE}" \
    --tag "jikim:${VERSION}" .
fi

if [[ "${RUN_SMOKE}" == true ]]; then
  "${SCRIPT_DIR}/smoke-offline.sh" "jikim:${VERSION}"
fi

printf '검증 완료: jikim %s\n' "${VERSION}"
