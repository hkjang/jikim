# 변경 기록

이 프로젝트는 [Keep a Changelog](https://keepachangelog.com/ko/1.1.0/) 형식을 따르며 버전은 [Semantic Versioning](https://semver.org/lang/ko/)을 사용합니다.

## [Unreleased]

### 계획

- OpenBao differential 호환성 비교 테스트 범위 확대
- PKI, 동적 데이터베이스 자격증명, Lease와 Raft/HA는 구현·검증 후 별도 프로파일로 제공

## [0.2.4] - 2026-09-07

### 추가

- 서비스 관리 → 시스템 설정 → 보안에 `신뢰 Reverse Proxy` 목록 추가. 등록한 CIDR 또는 IP에서 들어온 요청에 한해 `X-Forwarded-For` 체인을 오른쪽에서 왼쪽으로 판별해 실제 클라이언트 주소를 찾습니다

### 수정

- Reverse Proxy 뒤에서 감사 로그의 IP가 항상 Proxy 주소로 기록되고 로그인 실패 제한이 접속 주소 대신 Proxy 주소를 기준으로 묶이던 문제 수정. 목록을 비워 두면 이전과 동일하게 `X-Forwarded-For`를 무시하고 TCP 접속 주소만 사용합니다

## [0.2.3] - 2026-09-06

### 수정

- `PUT /api/v1/settings`가 보안·일반·AI 설정의 잘못된 타입 값을 200으로 저장한 뒤 읽을 때 조용히 버리던 오류 수정. `allow_local_login: "false"`처럼 문자열로 보낸 boolean이나 `session_timeout_minutes: "60"` 같은 문자열 숫자는 설정이 적용된 것처럼 보였지만 실제로는 기본값이 유지됐습니다. 이제 타입이 맞지 않으면 400으로 거부합니다

## [0.2.2] - 2026-09-05

### 수정

- `LIST /v1/secret/metadata/{prefix}`가 `_`나 `%`를 포함한 prefix에서 이웃 prefix의 key 이름까지 반환하던 오류 수정. prefix와 검색어를 SQL `LIKE` 패턴에 넣기 전에 와일드카드를 이스케이프하도록 변경했으며 Secret·감사 로그 검색도 이제 `_`와 `%`를 문자 그대로 대조합니다

## [0.2.1] - 2026-09-04

### 수정

- 관리자 설정의 Keycloak OIDC Issuer URL 입력 중 화면이 백지가 되던 오류 수정. 연결 테스트 결과가 있는 상태에서 입력하면 React가 setState 업데이터를 렌더 단계로 미루고, 그 시점에는 이미 비워진 synthetic event의 `currentTarget`을 읽어 렌더링이 중단됐습니다

### 변경

- 관리 설정, 사용자, 정책, 프로필, 키 화면의 입력 핸들러가 이벤트 값을 핸들러에서 먼저 읽고 상태 갱신에 전달하도록 정리해 같은 형태의 잠재 오류 제거
- 화면 렌더링 오류가 애플리케이션 전체를 내리지 않도록 라우팅 영역에 ErrorBoundary를 추가하고 다시 시도·새로 고침 복구 경로 제공
- setState 업데이터 안에서 `event.currentTarget`을 읽으면 lint가 실패하도록 규칙 추가

## [0.2.0] - 2026-09-01

### 추가

- OpenAPI 3.1 문서와 `/api/v1/capabilities` 기계 판독 지원 프로파일
- 정책 결과와 일치하는 Secret별 `capabilities` 및 관리자 정책 시뮬레이터
- AI SSE 실연결 테스트, 사용자별 동시·분당 제한, Bearer/API-Key/무인증 프로파일
- HMAC-SHA256 서명 Webhook 실제 전송, 이력, 연결 테스트와 수동 재시도 API
- 고정 경로 내부 CA bundle을 사용하는 Keycloak·AI·Webhook TLS 연동
- 실제 Docker 이미지의 전체 화면·새로 고침을 검증하는 Playwright 릴리스 게이트

### 변경

- Keycloak 역할 매핑은 정확한 `jikim-*` 값만 허용하고 충돌은 최소권한으로 처리
- OIDC callback URL을 관리자 설정값으로 고정하고 Keycloak RP-initiated logout 지원
- MCP Streamable HTTP stateless 계약을 2025-11-25/2025-06-18로 협상하며 Origin, JSON Content-Type, dual Accept와 protocol header를 검증
- MCP에 현재 사용자의 `access.check`를 추가하고 Secret 목록 권한 필터를 pagination 전에 적용
- OpenBao KV v2 제한 프로파일에 CAS, 버전 soft delete/undelete/destroy와 metadata hard delete 추가
- OIDC 사용자 출처·최근 로그인, 역할별 승인 건수와 실제 Secret 위험 점수 기반 대시보드 표시
- 릴리스 자산 재실행 시 기존 파일을 덮어쓰지 않는 불변 릴리스 정책

### 보안

- OIDC의 bare 역할명·경로형 그룹을 통한 관리자 권한 상승 차단
- MCP와 OpenBao Transit/KV 평문 응답을 구체적 감사 기록 실패 시 차단
- AI outbound redirect 차단, 설정 URL HTTPS 기본값과 명시적 내부 HTTP opt-in
- 민감 설정의 명시적 삭제와 Webhook 서명 키 회전 지원

## [0.1.0] - 2026-09-01

### 추가

- Go 단일 HTTP 서버와 React 한국어 관리 콘솔
- PostgreSQL 스키마 자동 마이그레이션과 부트스트랩 관리자
- 정확히 네 개의 애플리케이션 환경변수 계약
  - `POSTGRES_DSN`
  - `BOOTSTRAP_ADMIN`
  - `BOOTSTRAP_ADMIN_PASSWORD`
  - `ENCRYPTION_KEY`
- AES-256-GCM 기반 Secret, 사용자 키, 민감 설정과 승인 payload 저장 암호화
- 로컬 로그인, 역할, 세션, 프로필과 비밀번호 변경
- 애플리케이션 중심 Secret CRUD, 버전, 정책과 개인 키 회전
- 기본 비활성인 선택형 승인·반려 흐름과 요청자/승인자 분리
- 사용자·관리자·감사자 역할별 화면과 감사 이벤트 검색
- Keycloak OIDC Discovery, 연결 테스트와 Client 설정 관리 프로파일
- Secret 평문 패턴을 거부하는 OpenAI-compatible SSE AI 중계 및 최대 262,144 token 설정 고려
- JSON-RPC 2.0 MCP endpoint와 metadata·정책·감사·Transit 도구 프로파일
- API 탐색기와 cURL/MCP 예시
- 로그인 화면과 프로필 컨텍스트의 빌드 버전 노출
- `/healthz`, `/readyz`, `/api/v1/version`

### 제한 OpenBao 호환 프로파일

- `GET /v1/sys/health`
- Userpass login
- Token lookup-self, create, revoke-self 일부
- KV v2 data·metadata read/write/list/delete 일부
- Transit encrypt/decrypt 일부
- OpenBao 2.6.1 기준의 전체 기능·lease·TTL·error semantics 호환은 주장하지 않음

### 배포

- Node/Go/Alpine 멀티스테이지 Dockerfile
- non-root, read-only filesystem, capability 제거와 상태 확인 Compose 프로파일
- `jikim:v0.1.0` 이미지 계약
- `jikim-v0.1.0.tar.gz` 오프라인 번들과 `.sha256` 생성·검증 스크립트
- 내부 Docker 네트워크에서 PostgreSQL 기동 및 외부 egress 차단 스모크 테스트
- Go·React·Compose·문서 CI, 태그 기반 GitHub Release와 GitHub Pages 워크플로

### 문서

- 모바일 반응형 한국어 홍보 페이지
- SoftwareApplication·FAQ JSON-LD, Open Graph, canonical, sitemap, robots와 `llms.txt`
- 관리자, 사용자, API/MCP, 오프라인 설치, 보안, OpenBao 호환성 가이드
- 모든 React route를 대상으로 하는 PNG 화면 캡처 매니페스트

### 보안 주의사항

- 마스터 `ENCRYPTION_KEY` 자동 회전/재암호화는 v0.1.0 범위가 아니므로 값을 임의로 교체하면 안 됨
- 릴리스는 SHA-256 무결성을 제공하지만 이미지 서명, SBOM과 취약점 보고서는 별도 공급망 절차가 필요
- PKI, 동적 자격증명, Lease, Namespace, Seal/Unseal, Raft/HA와 Agent/Plugin은 v0.1.0 운영 지원 범위가 아님
- 오프라인 릴리스 이미지 아키텍처는 linux/amd64

[Unreleased]: https://github.com/hkjang/jikim/compare/v0.2.4...HEAD
[0.2.4]: https://github.com/hkjang/jikim/releases/tag/v0.2.4
[0.2.3]: https://github.com/hkjang/jikim/releases/tag/v0.2.3
[0.2.2]: https://github.com/hkjang/jikim/releases/tag/v0.2.2
[0.2.1]: https://github.com/hkjang/jikim/releases/tag/v0.2.1
[0.2.0]: https://github.com/hkjang/jikim/releases/tag/v0.2.0
[0.1.0]: https://github.com/hkjang/jikim/releases/tag/v0.1.0
