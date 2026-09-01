SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

VERSION := $(shell ./scripts/version.sh)
IMAGE := jikim:$(VERSION)
OUTPUT_DIR := dist
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: help version web build test verify docs-check docker smoke e2e package verify-bundle release-check compose-check

help: ## 사용 가능한 명령을 표시합니다.
	@awk 'BEGIN {FS = ":.*## "; printf "jikim 빌드 명령\n\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

version: ## 현재 릴리스 버전을 표시합니다.
	@printf '%s\n' "$(VERSION)"

web: ## React 정적 자산을 빌드합니다.
	npm --prefix web ci --no-audit --no-fund
	VITE_APP_VERSION="$(VERSION)" npm --prefix web run build

build: web ## 단일 Go 서버 바이너리를 빌드합니다.
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath \
		-ldflags="-s -w -X github.com/hkjang/jikim/internal/version.Version=$(VERSION) -X github.com/hkjang/jikim/internal/version.Commit=$(COMMIT) -X github.com/hkjang/jikim/internal/version.Date=$(BUILD_DATE)" \
		-o bin/jikim ./cmd/server

test: ## Go와 React 테스트를 실행합니다.
	go test $$(go list ./... | grep -v '/web/')
	npm --prefix web test -- --maxWorkers=1

verify: ## 소스, 테스트, 빌드, Compose 구성을 검증합니다.
	./scripts/verify.sh

docs-check: ## 홍보 페이지 SEO, 링크와 캡처 매니페스트를 검증합니다.
	node scripts/verify-docs.mjs

compose-check: ## 네 개의 필수 변수로 Compose 구성을 검증합니다.
	POSTGRES_DSN='postgres://user:pass@db:5432/jikim?sslmode=disable' \
	BOOTSTRAP_ADMIN='admin' \
	BOOTSTRAP_ADMIN_PASSWORD='verification-only' \
	ENCRYPTION_KEY='0123456789abcdef0123456789abcdef' \
	docker compose config --quiet

docker: ## $(IMAGE) 이미지를 로컬에서 빌드합니다.
	docker build \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--tag "$(IMAGE)" .

smoke: ## 외부 통신이 차단된 Docker 네트워크에서 스모크 테스트합니다.
	./scripts/smoke-offline.sh "$(IMAGE)"

e2e: ## 실제 Docker 이미지의 모든 화면과 새로 고침 동작을 브라우저로 검증합니다.
	./scripts/e2e-docker.sh "$(IMAGE)"

package: ## 오프라인 이미지 번들과 SHA-256 체크섬을 생성합니다.
	./scripts/package-offline.sh "$(OUTPUT_DIR)"

verify-bundle: ## 생성된 오프라인 번들의 무결성과 이미지 태그를 검증합니다.
	./scripts/verify-offline-bundle.sh "$(OUTPUT_DIR)/jikim-$(VERSION).tar.gz"

release-check: verify docker smoke e2e package verify-bundle ## 실제 릴리스 전 전체 검증을 수행합니다.
