# 변경 기록

이 프로젝트는 [Keep a Changelog](https://keepachangelog.com/ko/1.1.0/) 형식을 따르며 버전은 [Semantic Versioning](https://semver.org/lang/ko/)을 사용합니다.

## [Unreleased]

### 계획

- OpenBao differential 호환성 비교 테스트 범위 확대
- PKI, 동적 데이터베이스 자격증명, Lease와 Raft/HA는 구현·검증 후 별도 프로파일로 제공

## [0.2.21] - 2026-09-25

### 수정

- Webhook 연결 테스트가 저장소 장애를 엔드포인트 장애로 보고하던 오류 수정. `deliverWebhook`이 `CompleteWebhookDelivery`의 오류를 전송 오류 자리에 넣어 반환해, 엔드포인트가 `200`으로 정상 응답해도 delivery 기록에 실패하면 관리 화면이 `ok:false`와 `Webhook endpoint가 요청을 수락하지 않았습니다`를 내보냈습니다. 관리자가 자기 데이터베이스 장애를 보면서 멀쩡한 URL을 고치게 되던 문제입니다. 기록 실패는 이제 `logger.Warn`(delivery ID, event 종류, 요청 ID, 상태 코드만 — payload와 서명 키는 남기지 않습니다)으로만 남고, 전송 결과는 항상 엔드포인트가 답한 그대로 보고합니다. 저장소 장애 자체는 종전처럼 `/readyz`와 `500` 응답으로 드러납니다

### 변경

- `webhook.go`의 store 호출 세 곳(`WebhookConfig`, `CreateWebhookDelivery`, `CompleteWebhookDelivery`)을 기존 seam과 같은 nil-폴백 방식의 작은 메서드로 모았습니다. 설정하지 않으면 `s.store`를 그대로 부르므로 동작은 같으며, webhook 핸들러를 데이터베이스 없이 실제 엔드포인트로 시험할 수 있습니다. 서명, 헤더와 리다이렉트 비추종 계약은 변경하지 않았습니다
- 회귀 테스트 4개를 추가했습니다. 실제 `httptest` 엔드포인트와 운영 HTTP 클라이언트로 `webhookTest` 핸들러를 왕복해 `200`에 기록 실패가 겹친 경우와 전송 실패가 저장소 오류에 덮이던 경우를 고정합니다

### 문서

- 문서 프로파일을 v0.2.21로 갱신했으며, 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다
- 두 가이드 PDF(`docs/USER_GUIDE.pdf`, `docs/ADMIN_GUIDE.pdf`)는 v0.2.20과 같은 이유로 표지가 `v0.2.18`인 채로 남았습니다. 지금까지 쓰던 변환기(Markdown 표지 템플릿 + HeadlessChrome 인쇄)가 저장소에 없어 같은 판형으로 다시 구울 수 없었고, 다른 템플릿으로 바꿔 굽는 대신 기존 파일을 그대로 두었습니다. 본문 Markdown(`docs/USER_GUIDE.md`, `docs/ADMIN_GUIDE.md`)은 v0.2.21로 갱신되어 있습니다

## [0.2.20] - 2026-09-24

### 수정

- 새로 clone한 저장소에서 `make docker`가 실패하던 오류 수정. 이미지 빌드를 옮겨 담은 `scripts/build-image.sh`가 실행 권한 없이 커밋되어 있어 `Permission denied`로 멈췄습니다. 저장소에 기록된 파일 모드를 `100755`로 고쳐 다른 릴리스 스크립트(`verify.sh`, `smoke-offline.sh`, `e2e-docker.sh`)와 같은 권한을 갖도록 했습니다

### 변경

- 릴리스 검증 사슬의 성공 경로 출력을 줄였습니다. `npm ci`는 `--loglevel=error`, 웹 테스트는 `--reporter=dot`, Vite 빌드는 `--logLevel=warn`으로 돌고, Docker 빌드는 `scripts/build-image.sh`가 레이어 로그를 파일로 받아 두었다가 실패했을 때만 그대로 내보냅니다. 실패까지 감추는 `>/dev/null` 리다이렉트나 실패 레이어 로그를 지우는 `--quiet`는 쓰지 않으므로 실패 상세는 종전과 같습니다
- Playwright 콘솔 reporter를 `PLAYWRIGHT_REPORTER` 환경변수로 고를 수 있습니다(기본 `list`, `scripts/e2e-docker.sh`는 `dot`을 넘깁니다). HTML 보고서와 실패 시 상세 출력은 그대로 유지합니다

### 문서

- README 개발 검증 안내에 성공·실패 경로의 출력 차이와 `PLAYWRIGHT_REPORTER` 사용법을 적었습니다. 문서 프로파일을 v0.2.20으로 갱신했으며, 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다
- 두 가이드 PDF(`docs/USER_GUIDE.pdf`, `docs/ADMIN_GUIDE.pdf`)는 v0.2.19와 같은 이유로 표지가 `v0.2.18`인 채로 남았습니다. 지금까지 쓰던 변환기(Markdown 표지 템플릿 + HeadlessChrome 인쇄)가 저장소에 없어 같은 판형으로 다시 구울 수 없었고, 다른 템플릿으로 바꿔 굽는 대신 기존 파일을 그대로 두었습니다. 본문 Markdown(`docs/USER_GUIDE.md`, `docs/ADMIN_GUIDE.md`)은 v0.2.20으로 갱신되어 있습니다

## [0.2.19] - 2026-09-24

### 수정

- Vite 개발 서버에서 MCP SSO(OAuth) 메타데이터 주소가 404를 반환하던 오류 수정. Go 서버는 `/.well-known/oauth-protected-resource`와 `/.well-known/oauth-protected-resource/mcp`를 서빙하지만 개발 서버 프록시 목록에 없어, 관리 화면이 안내하는 메타데이터 주소와 `/mcp` 401 응답의 `resource_metadata` 주소가 개발 환경에서 백엔드에 닿지 못했습니다. `/.well-known` 전체가 아니라 해당 접두사만 좁게 전달하도록 프록시 항목 하나를 추가했으며, 실제 운영 `vite.config.ts`를 로드해 진짜 Vite 개발 서버와 native fetch로 도는 회귀 테스트 6개로 수정 전 404를 확인한 뒤 통과시켰습니다. 메타데이터와 `/mcp`가 같은 Host로 백엔드에 도착하는지도 함께 고정했습니다

### 문서

- CONTRIBUTING 개발 안내에 MCP SSO(OAuth)를 시험할 때 쓰는 메타데이터 주소를 추가했습니다. 문서 프로파일을 v0.2.19로 갱신했으며, 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다
- 두 가이드 PDF(`docs/USER_GUIDE.pdf`, `docs/ADMIN_GUIDE.pdf`)는 표지가 `v0.2.18`인 채로 남았습니다. 지금까지 쓰던 변환기(Markdown 표지 템플릿 + HeadlessChrome 인쇄)가 저장소에 없어 같은 판형으로 다시 구울 수 없었고, 다른 템플릿으로 바꿔 굽는 대신 기존 파일을 그대로 두었습니다. 본문 Markdown(`docs/USER_GUIDE.md`, `docs/ADMIN_GUIDE.md`)은 v0.2.19로 갱신되어 있습니다

## [0.2.18] - 2026-09-22

### 수정

- Vite 개발 서버에서 API 탐색기의 `/api/openapi.json` 링크가 브라우저 요청에는 HTML을, JSON 요청에는 404를 반환하던 오류 수정. 해당 경로를 기존 Go 백엔드 8080으로 전달하도록 프록시를 추가했습니다. 실제 Vite와 native fetch 회귀 테스트 9개로 확인했으며, Node 환경에서도 실행할 수 있도록 공통 테스트 setup의 브라우저 초기화만 조건부로 실행합니다

### 문서

- CONTRIBUTING에 Go 백엔드와 Vite 개발 서버의 실행 및 OpenAPI 확인 방법을 추가했습니다. 문서 프로파일과 두 가이드 PDF 표지를 v0.2.18로 갱신했으며, 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다

## [0.2.17] - 2026-09-21

### 수정

- AI 채팅 스트림에서 청크 소비자의 예외를 JSON 파싱 오류로 오인해 같은 이벤트를 원문으로 다시 전달하거나 성공으로 처리하던 오류 수정. `streamChat`의 예외 처리 범위를 `JSON.parse`로 좁혀 `onChunk`의 `Error`와 `SyntaxError`를 동일 객체로 호출자에게 전파하고, 해당 이벤트의 중복 호출과 뒤 이벤트 전달을 막았습니다. JSON `null`은 기존처럼 원문으로 전달하며, 소비자가 정상 처리하면 뒤 정상 이벤트도 계속 전달합니다. 실제 HTTP 서버와 native fetch를 통한 회귀 테스트로 확인했습니다

### 문서

- 문서 프로파일과 두 가이드 PDF 표지를 v0.2.17로 갱신했습니다. 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다

## [0.2.16] - 2026-09-20

### 수정

- 로그인 실패 표가 상한(10,000개)을 넘으면 잠긴 계정의 잠금이 밀려나던 오류 수정. `loginRateLimiter`는 초과 시 만료 항목을 지운 뒤에도 남으면 Go map 순회 순서대로 아무 항목이나 지웠고 잠긴(5회 도달) 항목도 대상이어서, 표적 계정을 잠근 같은 주소가 다른 username으로 실패를 흘리면 잠금이 지워져 5번이 다시 열렸습니다. 축출을 ① 만료 항목 → ② 아직 잠기지 않은 항목(방금 실패한 key 제외) → ③ 잠긴 항목만 남으면 만료 시각이 이른 순으로 결정적으로 지우게 바꿨습니다. 상한은 `loginFailureCapacity` 상수와 `capacity` 필드로 빼서 테스트가 낮출 수 있게 했고, 기본값 10,000개·5회/5분·`Retry-After`·auth 핸들러의 호출 위치는 그대로입니다

### 문서

- 문서 프로파일과 두 가이드 PDF 표지를 v0.2.16으로 갱신했습니다. 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다

## [0.2.15] - 2026-09-17

### 추가

- `/mcp`가 개인 API 토큰뿐 아니라 **Keycloak 액세스 토큰**으로도 열리는 **MCP SSO(OAuth)** 추가. MCP 인가 명세(2025-06-18+)의 리소스 서버 절반만 구현하며 설정은 `settings` 테이블의 새 키 `mcp`에 `{oauth:{enabled,resource,audience,scopes}}`로 저장합니다(기본값 꺼짐, 타입 검증). 켜지 않은 설치에서는 아무것도 달라지지 않습니다
- `GET /.well-known/oauth-protected-resource`와 `…/mcp`가 RFC 9728 보호 리소스 메타데이터를 인증 없이 봉투 없는 JSON(+CORS `*`)으로 내고, 꺼져 있으면 `404`입니다. `/mcp`의 `401`에만 `WWW-Authenticate: Bearer realm="jikim", resource_metadata="…"`(거부된 자격이면 `error="invalid_token"`)를 붙이고 REST·OpenBao 호환 `401`에는 붙이지 않습니다. 리소스 식별자는 `mcp.oauth.resource` → OIDC Callback URL(`oidc.redirect_url`)의 Origin + `/mcp` → 요청 `Host` 순으로 정합니다
- 같은 `Authorization: Bearer` 헤더에서 점 하나(`hvs.`/`jks.`)면 키, 세 조각이면 JWT로 갈라 `oidc.issuer_url`의 JWKS로 서명(RS/ES/PS만)·`iss`·`exp`·`nbf`를 검사하고 `typ=ID`·`cnf`·빈 `sub`는 거부합니다. 대상은 `aud`에 리소스 식별자가 있거나 `aud`/`azp`가 `mcp.oauth.audience`에 있어야 하며, 거부 메시지에 본 `aud`/`azp`와 적을 값을 넣습니다. `oidc.client_id`는 자동 허용하지 않아 웹 로그인 클라이언트의 토큰이 MCP를 열지 않습니다
- 계정은 웹 로그인이 이미 같은 issuer+subject로 연결한 활성 OIDC 계정만 찾고, 만들거나 username으로 대체하거나 role claim을 읽지 않습니다. SSO 주체는 키와 같은 정책·감사를 타되 `mcp.oauth.scopes` 상한(기본 `mcp:read`=조회 도구, `mcp:transit`=Transit 도구; 토큰 `scope`에 `mcp:*`가 있으면 교집합)을 받고 키는 좁히지 않습니다
- 스위치는 Keycloak OIDC가 꺼져 있거나 Issuer URL이 비면 저장 시 `400`으로 거부하고, 저장 뒤 OIDC가 꺼지면 조용히 잠들며 JWT가 들어올 때만 로그에 이유를 남깁니다. 관리 화면 OIDC 탭에 **MCP SSO(OAuth)** 카드(스위치·리소스 식별자·메타데이터 주소(읽기 전용)·허용 대상·범위)를 추가했습니다

### 변경

- 테스트가 실제 서명한 JWT를 만들면서 `github.com/go-jose/go-jose/v4`가 `go.mod`의 직접 의존성이 되었습니다(버전 변화 없음)

### 문서

- 관리자 가이드 3.3절에 `mcp.oauth` 설정 표, Keycloak 클라이언트·Audience 매퍼 절차, curl 확인과 거부 메시지별 조치 표를, API 및 MCP 가이드와 사용자 가이드에 "키 없이 SSO로 연결하기"를 추가했습니다. 문서 프로파일과 두 가이드 PDF 표지를 v0.2.15로 갱신했으며 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다

## [0.2.14] - 2026-09-17

### 수정

- `POST /v1/transit/decrypt/{key}`가 존재하지 않는 key를 `403` `permission denied`로 보고하던 오류 수정. `authorizeTransit`이 key 부재(`TransitPermission`의 `ErrNotFound`)를 encrypt에서만 create capability 확인으로 흡수하고 decrypt에서는 그대로 돌려줘 거부로 접히던 것을, `store.ErrNotFound`를 감싼 전용 sentinel `errTransitKeyNotFound`로 구분해 OpenBao와 같은 `400` `encryption key not found`로 응답합니다. 호출자는 이미 `transit/{key}` 경로 capability를 통과한 뒤이므로 key 이름을 알려 줘도 정책이 허용하지 않은 것을 드러내지 않으며, 정책 조회의 맨 `ErrNotFound`는 기존대로 `403`, MCP `transit.decrypt`는 기존 `404` not found를 유지합니다
- `POST /api/v1/login`과 `POST /v1/auth/userpass/login/{username}`이 비밀번호가 맞는 즉시 로그인 실패 창을 초기화하던 오류 수정. 로컬 로그인이 꺼진 계정(`403`)이나 보안 설정 조회 장애(`500`)도 거부이므로 실패 횟수를 유지하고, 세션을 만들기 직전에만 초기화합니다. 설정 조회는 `securityLoader` seam을 거쳐 순서를 DB 없이 검증합니다

### 변경

- 호환성 가이드에 key 부재는 `transit/{key}` 경로의 decrypt capability를 통과한 뒤에만 알려 주며, capability가 없으면 key 존재 여부와 무관하게 `403`임을 명시했습니다

### 문서

- 문서 프로파일과 두 가이드 PDF 표지를 v0.2.14로 갱신했습니다. 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다

## [0.2.13] - 2026-09-14

### 변경

- Go 의존성 갱신: `github.com/coreos/go-oidc/v3` 3.14.1 → 3.21.0, `github.com/jackc/pgx/v5` 5.9.2 → 5.11.0, `golang.org/x/oauth2` 0.30.0 → 0.37.0 (Dependabot #19, #6, #20). Keycloak OIDC 로그인과 PostgreSQL 접근 코드는 바뀌지 않았고 API와 OpenBao 호환 프로파일은 v0.2.12와 같습니다
- React 개발 의존성 갱신: `@testing-library/jest-dom` 6.8.0 → 7.0.1, `@testing-library/react` 16.3.0 → 16.3.3, `@types/react-dom` 19.2.2 → 19.2.5, `eslint-plugin-react-refresh` 0.4.24 → 0.5.5 (Dependabot #13, #10, #12, #9). 빌드된 관리 화면에는 포함되지 않습니다

### 문서

- 문서 프로파일과 두 가이드 PDF 표지를 v0.2.13으로 갱신했습니다. 화면 캡처는 실제로 찍은 `v0.2.9`를 그대로 가리킵니다

## [0.2.12] - 2026-09-13

### 추가

- Keycloak에 이미 로그인한 사람이 jikim을 열면 로그인 화면 없이 바로 본 화면으로 들어가는 **자동 로그인(조용한 SSO)** 추가. OIDC 설정에 `auto_login`(boolean, 기본값 꺼짐, 타입 검증)을 두고 관리 화면 OIDC 탭의 **자동 로그인(조용한 SSO)** 스위치로 켭니다. 켜지 않은 설치에서는 아무것도 달라지지 않습니다
- `GET /api/v1/oidc/login?prompt=none[&return_to=…]`가 이 설정이 켜졌을 때만 `prompt=none` 인가 요청을 보내고, 꺼져 있으면 `?prompt=none`이 붙어도 조용히 평범한 로그인으로 바꿉니다. 조용한 시도인지와 `return_to`는 봉인된 state 쿠키에 기록하므로 주소를 고쳐 흐름을 바꿀 수 없습니다. 콜백은 조용한 시도(쿠키 열기 성공·state 일치·10분 이내)가 제공자 오류를 받았을 때만 `/login?sso=none[&return_to=…]`으로 보내고, 그 밖의 오류는 기존 `/oidc/callback?error=…`를 유지합니다
- `return_to`는 `/`로 시작하고 `//`·`/\`로 시작하지 않는 같은 오리진 경로만 받으며 `/login`과 `/oidc/callback`은 돌아갈 자리로 받지 않습니다. 인가 코드 교환과 수동 로그인 뒤에도 같은 자리로 돌아갑니다
- 공개 설정 `GET /api/v1/settings/public`에 `oidc_auto_login` 추가. OIDC 자체가 꺼져 있으면 항상 `false`입니다
- 프런트엔드는 게스트가 보호된 화면을 열 때 공개 설정을 읽어 최상위 이동(`window.location.assign`)으로 한 번만 조용한 로그인을 시도합니다. 무한 루프는 `sessionStorage`의 **한 탭 세션에 한 번** 표시, 스스로 로그아웃한 뒤 다시 로그인할 때까지 억제, 콜백이 남기는 `?sso=none` 표시의 세 겹으로 막고, `sessionStorage`를 읽지 못하면 "이미 시도했다"로 취급해 막히는 쪽으로 실패합니다. 콜백·로그인·오류 화면과 `/api/*`·`/v1/*`·`/mcp`·probe·`/momento/*` 경로에서는 시도하지 않습니다

### 문서

- 관리자 가이드 3.3절에 `auto_login` 설정과 동작 순서, 루프 방지, `return_to` 검증 규칙과 켜기 전 확인할 Keycloak 세션 정책을 적고, 사용자 가이드 로그인 절에 자동 로그인이 켜졌을 때의 화면 흐름을 추가했습니다. 두 가이드 PDF를 같은 변환기로 다시 구웠습니다

## [0.2.11] - 2026-09-12

### 추가

- 관리자가 재배포 없이 관리 화면 **방문 추적** 탭에서 방문 추적 스크립트를 붙일 수 있는 체계 추가. 기본값은 꺼짐이며 켜기 전까지 어떤 화면에도 스니펫이 들어가지 않고 응답 헤더도 이전 릴리스와 같습니다. 제공자는 `momento`(첫 자리, 권장)·`ga4`·`gtm`·`matomo`·`custom`(직접 붙여넣기)이고, 설정은 다른 관리 설정과 같이 `settings` 테이블의 `tracking` 키에 저장하며 잘못된 타입·제공자·주소와 8KB를 넘는 스니펫은 `400`으로 거부합니다
- 스니펫을 붙이면서도 `'unsafe-inline'`을 쓰지 않도록 SPA 셸 응답마다 nonce를 만들어 스니펫의 모든 `<script>`에 붙이고 같은 nonce를 `script-src`에 넣습니다. 제공자와 붙여넣은 스니펫에서 읽은 출처를 `script-src`·`connect-src`·`img-src`에 더하고, 추적이 켜진 동안에만 `report-uri`를 넣어 브라우저가 신고한 차단 출처와 지시어를 메모리(100개 고리)에 기억합니다. 신고 수신 `POST /api/v1/tracking/csp-report`는 인증 없이 항상 `204`이며 감사 로그에 남기지 않습니다
- 차단 출처를 다루는 `admin` 전용 API 추가: 목록 `GET /api/v1/tracking/violations`, 기록 삭제 `DELETE /api/v1/tracking/violations`, 한 번 눌러 허용 목록에 더하는 `POST /api/v1/tracking/violations/allow`. 방문 추적 탭의 **정책이 차단한 출처** 표가 같은 기록을 보여 주고 **허용** 버튼으로 다음 화면 요청부터 정책에 반영합니다
- Momento 같은 오리진 프록시 `/momento/*` 추가(기본 ON). 추적이 켜져 있고 제공자가 Momento일 때만 열리며 그 밖에는 `404`입니다. `GET`·`HEAD`·`POST`만 넘기고 브라우저가 붙인 `Cookie`·`Authorization`·`X-Vault-Token`을 지운 뒤 전달하며, 수집기가 돌려준 `Set-Cookie`는 브라우저에 넘기지 않습니다. 본문은 64KB, 제한 시간은 10초입니다. 프록시를 켜 두면 브라우저는 jikim 오리진만 보므로 정책에 외부 출처가 등장하지 않습니다

### 변경

- 브라우저가 그리지 않는 `/api/*`, `/v1/*`, `/mcp`, `/healthz`, `/readyz`, `/momento/*` 응답의 콘텐츠 보안 정책을 `default-src 'none'`으로 더 좁혔습니다. 설정 저장소에 닿지 못하면 화면 응답은 추적 없이 이전 정책으로 내려가 로그인 화면이 함께 멈추지 않습니다
- `internal/store/settings.go`의 쓰이지 않던 `var _ = pgx.ErrNoRows` 제거

### 문서

- 관리자 가이드 3.7절에 방문 추적 설정 방법, CSP와 nonce 동작, 프록시 경계와 API 표를 추가하고 실제로 띄운 방문 추적 탭 캡처(`docs/screenshots/admin-settings-tracking.png`)를 실었습니다. 보안 기본값 표에 방문 추적과 수집기 내부 HTTP 허용 항목을 더했고 PDF를 같은 변환기로 다시 구웠습니다

## [0.2.10] - 2026-09-11

### 문서

- 실제 화면 캡처를 실은 사용자 가이드(`docs/USER_GUIDE.md`)와 관리자 가이드(`docs/ADMIN_GUIDE.md`)를 추가하고 같은 내용의 PDF를 함께 제공. 사용자 가이드는 로그인부터 첫 시크릿 저장까지, 화면별 사용법, 자주 하는 작업과 서비스가 실제로 돌려주는 오류 문구를 다루고, 관리자 가이드는 구성 요소, 릴리스 번들 설치, `internal/config/config.go` 기준 환경변수 전체 표, 역할과 정책, 운영, 서버가 실제로 남기는 로그 기준의 장애 대응과 바꿔야 할 보안 기본값을 다룹니다
- 기존 `docs/guides/user-guide.md`·`docs/guides/admin-guide.md`는 새 문서로 안내하는 포인터로 정리하고 README, 문서 허브, `guides/index.html`, `llms.txt`, 홍보 페이지와 앱 내 빠른 사용 가이드가 새 경로를 가리키도록 갱신
- 화면 갤러리를 `v0.2.9` 이미지로 전부 다시 캡처. 애플리케이션 목록, 정책 목록, 키 목록이 빈 화면으로 찍히지 않도록 E2E 전용 컨테이너와 전용 PostgreSQL에 데모 fixture(가짜 값, `example.internal` 주소만 사용)를 먼저 넣고, 감사 로그 화면은 이벤트 수만큼 길어지지 않도록 viewport 높이로 캡처

## [0.2.9] - 2026-09-10

### 수정

- `GET /v1/sys/health`가 저장소 상태를 확인하지 않고 항상 `200`과 `"sealed": false`를 반환하던 오류 수정. jikim의 Secret과 Transit key는 전부 PostgreSQL에 있으므로, 이제 호출마다 `/readyz`와 동일한 2초 제한으로 저장소 응답을 확인하고 닿지 못하면 OpenBao의 seal과 같은 운영 상태로 보아 `"sealed": true`와 `503`으로 응답합니다. OpenBao 클라이언트와 Load Balancer가 표준 probe로 쓰는 경로가 장애 노드를 정상 active로 보고하지 않으며, 원인(driver 내부 문자열, DSN, SQLSTATE)은 응답에 담지 않습니다

### 추가

- `GET /v1/sys/health`가 OpenBao probe 설정을 그대로 옮길 수 있도록 `activecode`와 `sealedcode` 쿼리 파라미터를 지원합니다. `100`~`599` 범위의 정수가 아니면 조용히 무시하지 않고 `400` `invalid activecode`·`invalid sealedcode`로 거부합니다. jikim은 standby나 uninitialized를 보고하지 않으므로 나머지 OpenBao probe 파라미터는 응답에 영향을 주지 않습니다

### 변경

- `/v1/sys/health`를 감사 대상에서 제외. 이미 제외된 `/healthz`·`/readyz`와 같은 비인증 probe이며 poll마다 감사 row가 쌓이던 것을 정리했습니다
- 저장소 응답 확인을 `/readyz`와 공유하는 `pingStorage` seam으로 정리
- 호환성 가이드에 `/v1/sys/health`의 저장소 판정과 probe 파라미터 계약 문서화

## [0.2.8] - 2026-09-10

### 수정

- 인증·세션 조회가 데이터베이스 장애까지 자격증명 거부와 세션 만료로 보고하던 오류 수정. `Authenticate`와 `SessionByToken`이 조회 실패를 원인과 무관하게 `ErrUnauthorized`로 접던 것을 행 없음만 sentinel로 남기도록 바꿔, 재시도 가능한 서버 장애가 종결 오류처럼 보이지 않습니다
- 관리 API 인증 미들웨어와 `GET /api/v1/session`이 조회 불가 상황에 `401`·`403`을 반환하던 오류 수정. 이제 `500`으로 응답합니다
- `/v1/*` 토큰 인증이 만료·폐기된 토큰과 조회 불가를 함께 `403` `permission denied`로 처리하던 오류 수정. 조회 자체에 실패하면 `500` `failed to look up token`으로 분리합니다
- `POST /api/v1/login`과 `POST /v1/auth/userpass/login/{username}`이 데이터베이스 장애를 로그인 실패 시도로 계산해 5분 창 동안 계정을 잠그던 오류 수정. 장애는 실패 시도에 계산하지 않고 각각 `500`으로 응답하며 driver 내부 문자열은 담지 않습니다

### 변경

- 호환성 가이드에 토큰 없음·만료·조회 불가의 응답 구분과 로그인 실패 제한 계산 기준 문서화

## [0.2.7] - 2026-09-10

### 수정

- OpenBao KV·Transit 핸들러와 MCP 도구가 capability 확인에 실패한 경우까지 거부로 처리하던 오류 수정. 데이터베이스 장애로 정책 조회 자체를 수행하지 못하면 이제 `403` `permission denied`가 아니라 `500` `failed to check permissions`로 응답해 재시도 가능한 서버 장애가 정책 오설정처럼 보이지 않으며, driver 내부 문자열은 담지 않습니다. store sentinel(`ErrNotFound`, `ErrForbidden`, `ErrUnauthorized`)은 기존대로 `403` `permission denied`, `ErrInvalid`는 `400`과 사유로 응답합니다
- MCP `transit.decrypt`가 capability 확인 실패까지 감사 기록에 `403`으로 남기던 오류 수정. 감사 `StatusCode`가 실제 실패 종류를 따릅니다
- `POST /v1/auth/userpass/login/{username}`이 보안 설정 조회 실패를 `403` `local login is disabled`로 보고하던 오류 수정. 이제 `500` `failed to read security settings`로 분리합니다
- MCP `secrets.metadata`가 버전 조회에 실패해도 `result`를 채워 부분 결과를 돌려주던 오류 수정

### 변경

- 호환성 가이드에 capability 판정 실패와 거부의 응답 구분 문서화

## [0.2.6] - 2026-09-09

### 수정

- MCP 도구 호출이 실패할 때 저장소 내부 오류 문자열을 그대로 `isError` 본문에 실어 보내던 오류 수정. 이제 store sentinel(`ErrInvalid`, `ErrNotFound`, `ErrForbidden`, `ErrUnauthorized`, `ErrConflict`)과 디스패치가 직접 만든 오류만 사유를 전달하고, 데이터베이스 장애처럼 그 밖의 실패는 `도구를 실행할 수 없습니다`로 접어 DSN host나 SQLSTATE가 MCP 클라이언트에 노출되지 않습니다
- `transit.decrypt` 도구가 서버 장애까지 감사 기록에 `400`으로 남기던 오류 수정. 디스패치 sentinel이 자기 상태 코드를 들고 다니도록 바꿔 감사 `StatusCode`가 실제 실패 종류를 따릅니다

### 변경

- `transit.encrypt` 도구가 테스트 seam을 우회해 store를 직접 호출하던 경로를 `transit.decrypt`와 동일한 내부 헬퍼로 정리
- API 가이드에 MCP 도구 실패 텍스트 계약 문서화

## [0.2.5] - 2026-09-09

### 수정

- `/v1/transit/encrypt/{key}`와 `/v1/transit/decrypt/{key}`가 저장소 오류를 종류와 무관하게 `400`으로 반환하고 내부 오류 문자열을 그대로 노출하던 오류 수정. 데이터베이스 장애처럼 서버 측 실패는 이제 `500`과 일반 메시지로 응답하고, 존재하지 않는 키는 OpenBao와 동일하게 `400` `encryption key not found`로 응답합니다. batch 경로도 항목별 `error`에 같은 판정을 적용하며 모든 항목이 실패했는데 서버 장애가 포함되면 `400` 대신 `500`을 반환합니다

## [0.2.4] - 2026-09-08

### 추가

- `/v1/transit/encrypt/{key}`와 `/v1/transit/decrypt/{key}`가 OpenBao Transit의 `batch_input`/`batch_results`를 지원합니다. 결과는 입력 순서를 유지하고 `reference`를 되돌려 주며, 항목별 실패는 `error`로 표시하고 모든 항목이 실패했을 때만 `400`을 반환합니다. jikim이 구현하지 않는 `context`, `nonce`, `associated_data`, `key_version`은 조용히 무시하지 않고 해당 항목의 오류로 처리하며, batch decrypt는 단건과 동일하게 감사 기록에 실패하면 평문을 반환하지 않습니다

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

[Unreleased]: https://github.com/hkjang/jikim/compare/v0.2.21...HEAD
[0.2.21]: https://github.com/hkjang/jikim/releases/tag/v0.2.21
[0.2.20]: https://github.com/hkjang/jikim/releases/tag/v0.2.20
[0.2.19]: https://github.com/hkjang/jikim/releases/tag/v0.2.19
[0.2.18]: https://github.com/hkjang/jikim/releases/tag/v0.2.18
[0.2.17]: https://github.com/hkjang/jikim/releases/tag/v0.2.17
[0.2.16]: https://github.com/hkjang/jikim/releases/tag/v0.2.16
[0.2.15]: https://github.com/hkjang/jikim/releases/tag/v0.2.15
[0.2.14]: https://github.com/hkjang/jikim/releases/tag/v0.2.14
[0.2.13]: https://github.com/hkjang/jikim/releases/tag/v0.2.13
[0.2.12]: https://github.com/hkjang/jikim/releases/tag/v0.2.12
[0.2.11]: https://github.com/hkjang/jikim/releases/tag/v0.2.11
[0.2.10]: https://github.com/hkjang/jikim/releases/tag/v0.2.10
[0.2.9]: https://github.com/hkjang/jikim/releases/tag/v0.2.9
[0.2.8]: https://github.com/hkjang/jikim/releases/tag/v0.2.8
[0.2.7]: https://github.com/hkjang/jikim/releases/tag/v0.2.7
[0.2.6]: https://github.com/hkjang/jikim/releases/tag/v0.2.6
[0.2.5]: https://github.com/hkjang/jikim/releases/tag/v0.2.5
[0.2.4]: https://github.com/hkjang/jikim/releases/tag/v0.2.4
[0.2.3]: https://github.com/hkjang/jikim/releases/tag/v0.2.3
[0.2.2]: https://github.com/hkjang/jikim/releases/tag/v0.2.2
[0.2.1]: https://github.com/hkjang/jikim/releases/tag/v0.2.1
[0.2.0]: https://github.com/hkjang/jikim/releases/tag/v0.2.0
[0.1.0]: https://github.com/hkjang/jikim/releases/tag/v0.1.0
