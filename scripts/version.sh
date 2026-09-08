#!/usr/bin/env bash
set -euo pipefail

readonly JIKIM_VERSION="v0.2.5"

if [[ ! "${JIKIM_VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  printf '잘못된 jikim 버전: %s\n' "${JIKIM_VERSION}" >&2
  exit 1
fi

printf '%s\n' "${JIKIM_VERSION}"
