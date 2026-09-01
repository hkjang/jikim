# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:24-bookworm-slim
ARG GO_IMAGE=golang:1.26.7-bookworm
ARG RUNTIME_IMAGE=alpine:3.22

FROM ${NODE_IMAGE} AS web-build
WORKDIR /src/web

ARG VERSION=v0.2.0
ENV VITE_APP_VERSION=${VERSION}

COPY web/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build && test -f dist/index.html

FROM ${GO_IMAGE} AS server-build
WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=v0.2.0
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./
COPY --from=web-build /src/web/dist ./web/dist

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath \
      -ldflags="-s -w \
        -X github.com/hkjang/jikim/internal/version.Version=${VERSION} \
        -X github.com/hkjang/jikim/internal/version.Commit=${COMMIT} \
        -X github.com/hkjang/jikim/internal/version.Date=${BUILD_DATE}" \
      -o /out/jikim ./cmd/server

FROM ${RUNTIME_IMAGE} AS runtime

ARG VERSION=v0.2.0
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 jikim \
    && adduser -S -D -H -u 10001 -G jikim jikim

WORKDIR /app
COPY --from=server-build --chown=10001:10001 /out/jikim /app/jikim
COPY --from=web-build --chown=10001:10001 /src/web/dist /app/web

LABEL org.opencontainers.image.title="jikim" \
      org.opencontainers.image.description="OpenBao 2.6.1 API limited-compatibility preview; scope: docs/guides/compatibility.md" \
      org.opencontainers.image.source="https://github.com/hkjang/jikim" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.licenses="Apache-2.0"

USER 10001:10001
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
    CMD wget -q -T 3 -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/app/jikim"]
