# jikim

**폐쇄망 운영을 고려한 애플리케이션 중심 Secrets & Identity Security Platform**

jikim은 Secret 값만 보관하는 도구를 넘어 애플리케이션, 소유자, 개인 키, 정책, 승인과 감사 흐름을 한곳에서 다루기 위한 Go + React 서비스입니다. 관리 화면은 한국어를 기본으로 하며 PostgreSQL에 저장되는 민감 데이터는 애플리케이션 계층에서 암호화합니다.

> 현재 버전은 `v0.2.2`입니다. OpenBao 전체 또는 99.9% 호환을 주장하지 않습니다. `/v1/*`는 구현된 핸들러와 제약을 공개하는 **제한 프리뷰**이며, OpenBao 2.6.1과의 differential 호환성 suite는 아직 구현되지 않았습니다. 정확한 상한은 [호환성 프로파일](docs/guides/compatibility.md)을 확인하십시오.

## v0.2.2 기능 프로파일

| 영역 | 상태 | 범위 |
| --- | --- | --- |
| 한국어 React 관리 화면 | 구현 | 로그인, 대시보드, Secret, 애플리케이션, 정책, 개인 키, 승인, 감사, 설정, AI, API 탐색기 |
| 로컬 인증·역할 | 구현 | `admin`, `manager`, `user`, `auditor`, 세션과 토큰 |
| Keycloak OIDC | 구현 프로파일 | Discovery, 고정 callback, PKCE·nonce, exact `jikim-*` 역할, RP logout와 연결 확인 |
| Secret 저장 | 구현 | 애플리케이션 중심 metadata, AES-256-GCM 암호화, 버전 관리, 정책 평가 |
| 개인 키 | 구현 | 사용자별 키 버전, 회전, 변경 가능한 작업 권한 |
| 승인 워크플로 | 구현 | 관리자 활성화 시 Secret 생성·변경·삭제 요청을 승인/반려; 기본 비활성 |
| 감사 | 구현 | 요청 ID, 사용자, 작업, 리소스, 상태, IP 중심 기록과 검색 |
| AI | 구현 프로파일 | OpenAI Chat Completions SSE, 실연결 확인, Bearer/API-Key/무인증, 요청 제한, `max_tokens` 최대 262,144 |
| Webhook | 구현 | HMAC-SHA256 서명 전송, 이벤트 필터, 연결 확인, 이력과 수동 재시도 |
| MCP | 구현 프로파일 | Streamable HTTP stateless, 2025-11-25/2025-06-18, 권한 확인·metadata·정책·감사·Transit 도구 |
| OpenBao API | 제한 프리뷰 | Health, Userpass, Token 일부, KV v2 CAS·버전 삭제/복구/폐기·metadata, Transit 일부; 전체 differential suite 미구현 |
| 오프라인 Docker | 구현 | `jikim:v0.2.2`, `jikim-v0.2.2.tar.gz`, SHA-256, egress 차단 스모크와 전체 화면 E2E |
| PKI·동적 DB 자격증명·Lease·Raft HA | 미지원/화면 프리뷰 | 후속 구현 대상이며 운영 지원으로 표시하지 않음 |

일부 메뉴는 전체 제품 방향을 보여주는 프리뷰 화면입니다. 화면이 존재한다는 이유만으로 해당 엔진이나 `/v1/*` API가 구현되었다고 판단하지 마십시오.

## 아키텍처

```text
브라우저 / API / bao 호환 클라이언트 / MCP 클라이언트
                         │
                         ▼
                 jikim Go server :8080
            ┌────────────┼─────────────┐
            │            │             │
        /api/v1/*      /v1/*         /mcp
        관리 API     제한 호환 API   JSON-RPC
            │            │             │
            └────────────┴─────────────┘
                         │
                         ▼
           암호화된 Secret + PostgreSQL
```

React 정적 자산은 릴리스 이미지의 `/app/web`에 포함되고 Go 서버가 SPA fallback을 제공합니다. 런타임에는 외부 CDN이나 npm/Go 저장소가 필요하지 않습니다.

## 빠른 시작

### 필수 값

jikim 애플리케이션은 정확히 다음 네 환경변수로 부트스트랩합니다.

```text
POSTGRES_DSN
BOOTSTRAP_ADMIN
BOOTSTRAP_ADMIN_PASSWORD
ENCRYPTION_KEY
```

- `BOOTSTRAP_ADMIN`은 3~128자입니다.
- `BOOTSTRAP_ADMIN_PASSWORD`는 12자 이상입니다.
- `ENCRYPTION_KEY`는 32바이트 원문, 64자리 hex 또는 32바이트 base64입니다.
- Keycloak, AI, 승인, 정책 등 일상적인 운영 설정은 관리자 화면에서 관리합니다.

### Docker Compose

`docker-compose.yml`은 PostgreSQL을 번들링하지 않습니다. 사내 표준 PostgreSQL을 먼저 준비합니다.

```bash
export POSTGRES_DSN='postgres://jikim_app:REDACTED@postgres.internal:5432/jikim?sslmode=verify-full'
export BOOTSTRAP_ADMIN='bootstrap-admin'
export BOOTSTRAP_ADMIN_PASSWORD='REDACTED-AT-LEAST-12-CHARS'
export ENCRYPTION_KEY='REDACTED-64-HEX-CHARS'
docker compose up --detach
```

사내 CA로 Keycloak·AI·Webhook TLS를 검증할 때는 인증서를 `certs/internal-ca.crt`에 두고 추가 환경변수 없이 다음 override를 함께 사용합니다.

```bash
docker compose -f docker-compose.yml -f docker-compose.internal-ca.yml up --detach
```

상태 확인:

```bash
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8080/v1/sys/health
```

첫 로그인 후 로그인 화면과 프로필 컨텍스트 메뉴의 버전이 이미지 태그와 같은지 확인합니다.

## 오프라인 반입

GitHub Release 산출물:

```text
jikim-v0.2.2.tar.gz
jikim-v0.2.2.tar.gz.sha256
```

```bash
sha256sum --check jikim-v0.2.2.tar.gz.sha256
docker load --input jikim-v0.2.2.tar.gz
docker image inspect jikim:v0.2.2
```

상세 절차와 TLS·백업·롤백 경계는 [오프라인 설치 가이드](docs/guides/offline-install.md)를 참조하십시오.

## 로컬 빌드와 검증

필요 도구는 Go(`go.mod` 버전), Node.js 24/npm, Docker/Compose입니다.

```bash
./scripts/verify.sh              # Go·React·문서·Compose
./scripts/verify.sh --docker     # 이미지 빌드 포함
docker pull postgres:17-alpine  # 스모크 테스트용 이미지를 연결망에서 미리 준비
./scripts/verify.sh --smoke      # 내부 Docker 네트워크 + egress 차단 검증
make e2e                        # 실제 이미지의 모든 화면·새로 고침 브라우저 검증
```

실제 릴리스 전 로컬 계약:

```bash
make release-check
```

오프라인 패키지만 만들 때:

```bash
make docker
make package
make verify-bundle
```

태그 `v0.2.2`을 push하면 릴리스 워크플로가 태그·소스 버전 일치, linux/amd64 이미지, 상태 API, 내부 PostgreSQL, egress 차단, 전체 화면 Playwright와 번들 SHA-256을 검증한 뒤 GitHub Release를 생성합니다. 이미 발행된 릴리스 자산은 덮어쓰지 않습니다.

## API와 MCP

- jikim 관리 API: `/api/v1/*`
- OpenBao 제한 호환 API: `/v1/*`
- MCP JSON-RPC 2.0: `POST /mcp`
- OpenAPI 3.1: `/api/openapi.json`
- 구현 역량 조회: `/api/v1/capabilities`
- 대화형 확인: 관리 화면의 **API 탐색기**

API 응답과 요청 ID, SSE 스트리밍, MCP 도구 목록은 [API 및 MCP 가이드](docs/guides/api-guide.md)를 참조하십시오.

## 보안 경고

`ENCRYPTION_KEY`를 잃으면 저장된 암호문을 복구할 수 없습니다. 데이터 재암호화 절차 없이 이 값을 바꾸면 기존 Secret을 읽을 수 없습니다. PostgreSQL 백업과 키 백업을 분리하고 반드시 복구 시험을 수행하십시오.

또한 다음을 지킵니다.

- 운영에서는 TLS Reverse Proxy와 PostgreSQL TLS 사용
- 컨테이너 egress를 PostgreSQL과 승인된 Keycloak/AI 주소로 제한
- Secret 평문, 토큰, DSN, Client Secret을 로그·AI·이슈에 넣지 않음
- 관리자·복호화·키 회전 권한 최소화
- 이미지 체크섬, digest와 배포 버전 기록

취약점은 공개 Issue가 아닌 [`SECURITY.md`](SECURITY.md)의 비공개 절차로 신고하십시오.

## 문서

- [문서 허브](docs/guides/README.md)
- [관리자 가이드](docs/guides/admin-guide.md)
- [사용자 가이드](docs/guides/user-guide.md)
- [API 및 MCP](docs/guides/api-guide.md)
- [오프라인 설치](docs/guides/offline-install.md)
- [보안 운영](docs/guides/security.md)
- [OpenBao 호환성](docs/guides/compatibility.md)
- [화면 캡처 계약](docs/screenshots/README.md)
- [홍보 페이지](https://hkjang.github.io/jikim/)

## 기여와 라이선스

기여 전 [`CONTRIBUTING.md`](CONTRIBUTING.md)를 확인하십시오. jikim은 [Apache License 2.0](LICENSE)으로 배포됩니다.
