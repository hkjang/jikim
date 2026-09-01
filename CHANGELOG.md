# 변경 기록

이 프로젝트는 [Keep a Changelog](https://keepachangelog.com/ko/1.1.0/) 형식을 따르며 버전은 [Semantic Versioning](https://semver.org/lang/ko/)을 사용합니다.

## [Unreleased]

### 계획

- v0.1.0 호환성 비교 테스트 범위 확대
- PKI, 동적 데이터베이스 자격증명, Lease와 Raft/HA는 구현·검증 후 별도 프로파일로 제공

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

[Unreleased]: https://github.com/hkjang/jikim/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hkjang/jikim/releases/tag/v0.1.0
