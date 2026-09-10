# API 및 MCP 가이드

이 문서는 jikim `v0.2.8` HTTP 인터페이스의 공통 계약을 설명합니다. 구현의 최종 기준은 해당 릴리스 소스와 자동 테스트이며, 이 문서에 없는 OpenBao API가 동작한다고 가정하면 안 됩니다.

## API 경계

| 경로 | 역할 | 호환성 의미 |
| --- | --- | --- |
| `/v1/*` | OpenBao 제한 프리뷰 | 구현된 핸들러와 알려진 제약만 문서화; differential suite 미구현 |
| `/api/v1/*` | jikim 관리·사용자 API | jikim 고유 계약, OpenBao API가 아님 |
| `/mcp` | Stateless MCP JSON-RPC 2.0 endpoint | 현재 세션의 정책과 감사를 적용하며 엄격한 HTTP 헤더 계약 사용 |
| `/healthz` | 프로세스 상태 | 인증 없음 |
| `/readyz` | PostgreSQL 포함 준비 상태 | 인증 없음 |

v0.2.8은 OpenBao 전체 API 호환이나 99.9% 호환을 주장하지 않으며 OpenBao 2.6.1 differential suite도 아직 없습니다. [호환성 가이드](compatibility.md)를 먼저 확인하십시오.

## 인증

로그인 요청:

```bash
curl --request POST 'https://jikim.example/api/v1/auth/login' \
  --header 'Content-Type: application/json' \
  --data '{"username":"alice","password":"REDACTED"}'
```

브라우저 로그인 응답은 사용자 정보와 만료 시각을 반환하고, 인증 정보는 `HttpOnly`, `SameSite=Strict` 세션 쿠키로만 전달합니다. 자동화용 Bearer 토큰은 로그인 응답이 아니라 인증 후 `POST /api/v1/tokens`에서 발급하며 생성 시 한 번만 표시됩니다. 이후 API 요청은 다음 중 하나를 사용합니다.

```http
Authorization: Bearer <token>
```

```http
X-Vault-Token: <token>
```

OpenBao Userpass 프로파일은 `/v1/auth/userpass/login/:username` 응답의 `auth.client_token`을 사용합니다. HTTPS 요청에서는 브라우저 쿠키의 Secure 속성이 적용됩니다. 토큰을 URL, 로그, 이슈 또는 코드 저장소에 넣지 마십시오.

## Keycloak OIDC 계약

OIDC가 활성화되면 다음 순서로 로그인합니다.

1. `GET /api/v1/oidc/login?redirect_uri=/oidc/callback`으로 Authorization Code Flow를 시작합니다.
2. jikim이 state, nonce와 PKCE S256 verifier를 보호된 10분 수명의 쿠키에 저장하고 공급자로 이동시킵니다.
3. 공급자는 관리자가 등록한 절대 `redirect_url`(예: `https://jikim.example/api/v1/oidc/callback`)로 돌아옵니다.
4. jikim이 ID Token의 issuer, audience, 서명, nonce를 검증한 뒤 브라우저를 상대 경로 `/oidc/callback?code=...`로 이동시킵니다.
5. SPA가 일회용 코드를 `POST /api/v1/oidc/exchange`로 교환하면 HttpOnly 세션 쿠키가 설정됩니다.

| Endpoint | 인증 | 현재 계약 |
| --- | --- | --- |
| `GET /api/v1/oidc/config` | 없음 | enabled, issuer, client ID, scopes와 로그인 URL만 공개. Client Secret 제외 |
| `POST /api/v1/oidc/test` | `admin` | discovery 문서 endpoint 확인만 수행. Client Secret의 token 교환 유효성까지 시험하지 않음 |
| `GET /api/v1/oidc/login` | 없음 | state·nonce·PKCE를 만들고 공급자 authorization endpoint로 이동 |
| `GET /api/v1/oidc/callback` | 없음 | state·nonce·ID Token 검증 후 SPA용 일회용 코드 발급 |
| `POST /api/v1/oidc/exchange` | 없음 | 일회용 코드를 로컬 HttpOnly 세션으로 교환 |
| `GET /api/v1/oidc/logout` | 세션 | 로컬 로그아웃 후 가능한 경우 공급자 RP logout으로 이동 |

OIDC를 활성화하려면 `redirect_url`을 반드시 저장해야 합니다. URL은 `http` 또는 `https` 절대 URL이고 path가 정확히 `/api/v1/oidc/callback`이어야 하며 userinfo, query와 fragment는 허용되지 않습니다. 비-loopback HTTP는 관리자가 `allow_insecure_http`를 명시한 경우에만 저장할 수 있습니다. 요청 `Host`로 callback URL을 자동 생성하지 않습니다. 브라우저가 로그인 시작 시 같은 Origin의 절대 SPA URL을 보내더라도 서버는 최종 SPA 이동 대상을 상대 `/oidc/callback`으로 정규화하고, 다른 Origin은 거부합니다.

공급자가 `error`를 반환하면 원문 오류 설명을 SPA redirect query에 반영하지 않고 `/oidc/callback?error=oidc_provider_error&error_description=...` 상대 경로로 이동합니다.

역할·그룹 claim에서 로컬 역할로 인정하는 값은 다음 네 문자열뿐입니다. 값은 대소문자를 포함해 정확히 일치해야 합니다.

| OIDC claim 값 | jikim 역할 |
| --- | --- |
| `jikim-admin` | `admin` |
| `jikim-manager` | `manager` |
| `jikim-auditor` | `auditor` |
| `jikim-user` | `user` |

`admin`, `/org/admin`, `/org/jikim-admin`, `JIKIM-ADMIN` 같은 별칭이나 path 형태는 승격하지 않습니다. 서로 다른 전용 역할이 동시에 매핑되면 배열 순서에 의존하지 않고 `user`로 낮춥니다. 관리자가 role/group claim 동기화를 설정했는데 인식 가능한 값이 없을 때도 기존의 높은 로컬 역할을 유지하지 않고 `user`로 동기화합니다.

인증된 사용자는 `GET /api/v1/oidc/logout`으로 로그아웃할 수 있습니다. 서버는 로컬 세션을 먼저 폐기하고 쿠키를 지운 뒤, OIDC 사용자이고 discovery 문서에 `end_session_endpoint`가 있을 때 `client_id`와 설정된 callback Origin의 `/login`을 사용해 RP-initiated logout으로 이동합니다. 공급자 연결 또는 discovery가 실패해도 로컬 로그아웃은 유지됩니다. 이 버전은 `id_token_hint`, OIDC back-channel logout과 공급자 세션 알림을 구현하지 않습니다.

## 공통 응답

jikim 관리 API의 성공 응답은 주로 `data` envelope를 사용합니다.

```json
{
  "data": {
    "name": "jikim",
    "version": "v0.2.8",
    "commit": "...",
    "date": "..."
  }
}
```

오류 응답:

```json
{
  "error": {
    "code": "forbidden",
    "message": "이 작업을 수행할 권한이 없습니다",
    "request_id": "req_..."
  }
}
```

모든 요청의 `X-Request-ID` 응답 헤더를 기록하십시오. 클라이언트가 128자 이하의 `X-Request-ID`를 보내면 추적에 재사용할 수 있습니다.

JSON 요청 본문은 기본적으로 2 MiB를 초과할 수 없고 알 수 없는 필드는 거부됩니다. 한 요청에는 하나의 JSON 값만 전송합니다.

## 상태 및 버전

```bash
curl --fail 'https://jikim.example/healthz'
curl --fail 'https://jikim.example/readyz'
curl --fail 'https://jikim.example/api/v1/version'
curl --fail 'https://jikim.example/v1/sys/health'
```

`/healthz` 성공이 데이터베이스 준비를 의미하지는 않습니다. 트래픽 투입 판단에는 `/readyz`를 사용합니다. OpenBao 호환 클라이언트와 Load Balancer는 같은 목적으로 `/v1/sys/health`를 사용할 수 있습니다. 이 경로는 PostgreSQL에 닿지 못하면 `"sealed": true`와 `503`을 반환하고, `?activecode=`·`?sealedcode=`로 probe가 기대하는 상태 코드를 지정할 수 있습니다.

## jikim API v0.2.8 요약

다음 표는 관리 API의 주요 그룹입니다. 역할과 세부 필드는 서버 검증을 따릅니다.

| 그룹 | 대표 경로 | 주요 동작 |
| --- | --- | --- |
| 인증·프로필 | `/api/v1/auth/*`, `/api/v1/me` | 로그인, 로그아웃, 프로필·비밀번호 |
| Secret | `/api/v1/secrets` | 목록, 생성, 상세, 변경, 삭제, 버전 |
| 애플리케이션 | `/api/v1/applications` | 서비스 카탈로그 CRUD |
| 정책·사용자 | `/api/v1/policies`, `/api/v1/users` | 정책·계정·할당 관리 |
| 개인·서비스 키 | `/api/v1/keys` | 목록, 회전, 권한 변경 |
| 승인 | `/api/v1/approvals` | 요청 목록, 승인, 반려 |
| 감사 | `/api/v1/audit` | 필터 가능한 감사 이벤트 |
| 설정 | `/api/v1/settings` | 관리자 전용 운영 설정 |
| AI | `/api/v1/ai/chat` | SSE 스트리밍 응답 |

승인 워크플로가 활성화되면 Secret 쓰기 요청은 즉시 결과 대신 `202 Accepted`와 승인 요청을 반환할 수 있습니다. 클라이언트는 `200/201`만 성공으로 고정하지 말고 `202`를 처리해야 합니다.

## 목록 페이지

목록 API는 다음 쿼리를 공통적으로 사용할 수 있습니다.

```text
?q=payment&limit=50&offset=0
```

`limit`과 `offset`에는 음수가 아닌 정수만 사용하십시오. 대량 데이터는 작은 페이지로 나누고 응답에 Secret 평문이 포함되는지 확인한 뒤 로그 정책을 정합니다.

## AI 스트리밍

AI 요청은 Server-Sent Events(SSE)를 기본으로 합니다.

```bash
curl --no-buffer --request POST 'https://jikim.example/api/v1/ai/chat' \
  --header "Authorization: Bearer ${JIKIM_TOKEN}" \
  --header 'Content-Type: application/json' \
  --header 'Accept: text/event-stream' \
  --data '{
    "messages":[{"role":"user","content":"현재 보안 상태를 요약해줘"}],
    "max_tokens":4096
  }'
```

각 이벤트의 `data:` payload를 순서대로 소비하고 `[DONE]`에서 종료합니다. 프록시에서는 response buffering을 끄고 충분한 idle timeout을 설정합니다.

- 서비스 설계 상한은 최대 `262144` 토큰을 고려합니다.
- 실제 허용값은 관리자 설정과 연결 모델의 출력 상한 중 가장 작은 값입니다.
- 운영 컨텍스트는 서버가 호출자의 역할과 집계 데이터로 구성하며 클라이언트 제공 `context`는 사용하지 않습니다.
- Secret 평문, 키 원문, 토큰, OIDC Client Secret은 AI 메시지에 포함하지 않습니다.
- 사용자가 연결을 취소하면 HTTP 요청도 취소해 불필요한 모델 실행을 중단합니다.

## MCP

MCP endpoint는 세션을 생성하지 않는 Stateless Streamable HTTP 프로파일입니다. `GET /mcp`는 SSE 연결을 열지 않고 `405 Method Not Allowed`와 `Allow: POST`를 반환합니다. 모든 실제 호출은 인증된 `POST /mcp`를 사용합니다.

초기화 요청은 다음과 같습니다. `Content-Type`은 `application/json`이어야 하며 `Accept`에는 두 media type을 모두 명시해야 합니다.

```bash
curl --request POST 'https://jikim.example/mcp' \
  --header "Authorization: Bearer ${JIKIM_TOKEN}" \
  --header 'Content-Type: application/json' \
  --header 'Accept: application/json, text/event-stream' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"initialize",
    "params":{
      "protocolVersion":"2025-11-25",
      "capabilities":{},
      "clientInfo":{"name":"offline-client","version":"1.0.0"}
    }
  }'
```

지원하는 협상 버전은 `2025-11-25`와 `2025-06-18`입니다. `initialize`에는 `MCP-Protocol-Version` 헤더를 생략할 수 있지만, 이후 요청에는 클라이언트가 선택한 지원 버전을 헤더로 보내야 합니다. 서버는 stateless이므로 이전 initialize 응답과 헤더를 서버측 session으로 묶어 비교하지 않습니다. 지원하지 않는 body의 `protocolVersion`에는 최신 지원 버전을 제안하며, 클라이언트는 이를 수용할 수 없으면 연결을 중단해야 합니다.

```bash
curl --request POST 'https://jikim.example/mcp' \
  --header "Authorization: Bearer ${JIKIM_TOKEN}" \
  --header 'Content-Type: application/json' \
  --header 'Accept: application/json, text/event-stream' \
  --header 'MCP-Protocol-Version: 2025-11-25' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

브라우저가 `Origin`을 보내면 요청 scheme과 host에 정확히 일치해야 하며 다른 Origin은 `403`으로 거부됩니다. 비브라우저 클라이언트는 `Origin`을 생략할 수 있습니다. 잘못된 Content-Type은 `415`, 두 Accept 값 중 하나라도 빠지면 `406`, 후속 요청의 protocol 헤더가 없거나 지원 범위 밖이면 `400`입니다. JSON-RPC notification은 `id`를 생략하며 서버는 `202 Accepted`와 빈 본문을 반환합니다.

MCP는 REST 권한을 우회하는 관리 채널이 아닙니다. 현재 도구 계약은 다음과 같습니다.

| 도구 | 입력 | 권한과 반환 범위 |
| --- | --- | --- |
| `dashboard.get` | 없음 | 현재 역할에 허용된 대시보드 요약. 일반 사용자의 최근 감사 내역은 제외 |
| `secrets.list` | 선택 `query` | `list` 권한을 먼저 적용한 최대 100개 metadata; Secret 값 없음 |
| `secrets.metadata` | `path` | 해당 정확한 path의 `read` 권한 필요; 버전·위험도 metadata만 반환 |
| `policies.list` | 없음 | `admin`, `manager`, `auditor`만 최대 100개 조회 |
| `audit.search` | 선택 `query` | `admin`, `manager`, `auditor`만 최대 100개 조회 |
| `access.check` | `path`, `capability` | 현재 로그인 사용자 자신의 유효 권한만 시뮬레이션. `user_id` 지정 불가 |
| `transit.encrypt` | `key`, `plaintext` | 이름 있는 키의 `encrypt` 권한과 키 권한 필요; plaintext는 base64 |
| `transit.decrypt` | `key`, `ciphertext` | `decrypt` 권한과 키 권한 필요; tool/key가 식별되는 감사 저장 성공 전에는 base64 평문을 공개하지 않음 |

- 사용자 세션과 동일하게 인증
- 도구별 정책 평가
- 읽기와 변경 도구 구분
- Secret 평문을 반환하는 도구는 최소화
- 모든 호출을 요청 ID와 사용자 기준으로 감사

지원 도구 목록은 실행 중인 서버의 `tools/list` 결과가 기준입니다. 알 수 없는 도구·인자, 빠진 필수 인자와 잘못된 타입은 JSON-RPC `-32602` 오류가 됩니다. 현재 endpoint는 JSON 또는 notification의 빈 응답만 반환하며, 서버 발 SSE event stream, `Mcp-Session-Id`, 재개·재전송, JSON-RPC batch를 제공하지 않습니다.

도구 실행이 실패하면 `isError=true` 본문에 권한 없음·대상 없음·잘못된 요청 값처럼 호출자가 조치할 수 있는 사유만 담깁니다. 데이터베이스 장애 같은 서버 측 실패는 `도구를 실행할 수 없습니다`로 일반화되며 드라이버 오류 문자열을 노출하지 않으므로, 원인 분석은 서버 로그와 감사 기록으로 하십시오.

## OpenBao KV v2 제한 프로파일

KV v2 mount는 `secret`으로 고정되어 있으며 mount 생성·이동·tune API는 없습니다. 모든 경로는 `X-Vault-Token` 또는 지원되는 jikim 토큰으로 인증하고 로컬 policy capability를 적용합니다.

| 요청 | 현재 동작 |
| --- | --- |
| `GET /v1/secret/data/{path}` | 최신 버전 조회. `?version=N`으로 양의 정수 버전 선택. soft-delete·destroy 버전은 `404` |
| `POST` 또는 `PUT /v1/secret/data/{path}` | `{"data":{...},"options":{"cas":N}}` 형식으로 새 버전 생성 |
| `DELETE /v1/secret/data/{path}` | 현재 버전 soft-delete, `204` |
| `POST /v1/secret/delete/{path}` | `{"versions":[1,2]}`의 미파괴 버전을 soft-delete, `204` |
| `POST /v1/secret/undelete/{path}` | 지정한 soft-delete 버전을 다시 읽을 수 있게 복구, `204` |
| `PUT /v1/secret/destroy/{path}` | 지정 버전의 암호문과 nonce를 비우고 영구 파괴 표시, `204` |
| `GET /v1/secret/metadata/{path}` | 버전별 생성·삭제·파괴 상태와 고정된 제한 설정값 조회 |
| `LIST /v1/secret/metadata/{prefix}` | 직계 key/폴더 이름 조회. `GET ...?list=true`도 지원 |
| `DELETE /v1/secret/metadata/{path}` | key와 모든 버전을 영구 삭제, `204` |

CAS 의미는 `options.cas` 생략 시 현재 버전과 무관하게 새 버전 생성, `0`이면 key가 없을 때만 생성, 양수이면 현재 버전과 정확히 일치할 때만 갱신입니다. 불일치와 동시 CAS 경쟁 패자는 `400`의 OpenBao형 `errors` envelope를 받습니다. 전역 `cas_required` 설정은 없으며 metadata에는 항상 `cas_required=false`가 표시됩니다.

이 구현은 자동 version pruning과 `max_versions`, `delete_version_after`, metadata CAS, metadata 갱신 API를 제공하지 않습니다. metadata의 해당 값은 각각 `0`, `0s`, `false`로 고정되고 `oldest_version`은 `0`입니다. 존재하지 않는 개별 version 번호는 delete/undelete/destroy에서 별도 오류 없이 무시될 수 있으므로 호출 후 metadata를 다시 확인하십시오. 자세한 비호환 범위는 [호환성 프로파일](compatibility.md)을 따릅니다.

## OpenBao 클라이언트 연결 전 확인

1. 클라이언트가 호출하는 모든 `/v1/*` 경로를 목록화합니다.
2. [호환성 프로파일](compatibility.md)에 포함되어 있는지 확인합니다.
3. 테스트 환경에서 상태 코드, 필드, TTL, 오류를 비교합니다.
4. 지원하지 않는 경로가 있으면 `/api/v1/*`로 자동 대체하지 않습니다.
5. 쓰기 요청은 백업과 감사 확인 후 운영에 적용합니다.
