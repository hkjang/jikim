# API 및 MCP 가이드

이 문서는 jikim `v0.1.0` HTTP 인터페이스의 공통 계약을 설명합니다. 구현의 최종 기준은 해당 릴리스 소스와 자동 테스트이며, 이 문서에 없는 OpenBao API가 동작한다고 가정하면 안 됩니다.

## API 경계

| 경로 | 역할 | 호환성 의미 |
| --- | --- | --- |
| `/v1/*` | OpenBao 제한 프리뷰 | 구현된 핸들러와 알려진 제약만 문서화; differential suite 미구현 |
| `/api/v1/*` | jikim 관리·사용자 API | jikim 고유 계약, OpenBao API가 아님 |
| `/mcp` | JSON-RPC 2.0 MCP endpoint | 현재 세션의 정책과 감사를 그대로 적용 |
| `/healthz` | 프로세스 상태 | 인증 없음 |
| `/readyz` | PostgreSQL 포함 준비 상태 | 인증 없음 |

v0.1.0은 OpenBao 전체 API 호환이나 99.9% 호환을 주장하지 않으며 OpenBao 2.6.1 differential suite도 아직 없습니다. [호환성 가이드](compatibility.md)를 먼저 확인하십시오.

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

## 공통 응답

jikim 관리 API의 성공 응답은 주로 `data` envelope를 사용합니다.

```json
{
  "data": {
    "name": "jikim",
    "version": "v0.1.0",
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

`/healthz` 성공이 데이터베이스 준비를 의미하지는 않습니다. 트래픽 투입 판단에는 `/readyz`를 사용합니다.

## jikim API v0.1.0 요약

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

MCP 클라이언트는 같은 HTTPS Origin의 `POST /mcp`에 JSON-RPC 2.0 요청을 보냅니다.

```bash
curl --request POST 'https://jikim.example/mcp' \
  --header "Authorization: Bearer ${JIKIM_TOKEN}" \
  --header 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

MCP는 REST 권한을 우회하는 관리 채널이 아닙니다.

- 사용자 세션과 동일하게 인증
- 도구별 정책 평가
- 읽기와 변경 도구 구분
- Secret 평문을 반환하는 도구는 최소화
- 모든 호출을 요청 ID와 사용자 기준으로 감사

지원 도구 목록은 실행 중인 서버의 `tools/list` 결과가 기준입니다. 문서에 없는 도구 이름을 추측해 호출하지 마십시오.

v0.1.0 프로파일에는 `dashboard.get`, `secrets.list`, `secrets.metadata`, `policies.list`, `audit.search`, `transit.encrypt`, `transit.decrypt`가 정의되어 있습니다. Secret 목록·metadata 도구는 값을 반환하지 않는 것이 기본이며, Transit 복호화 결과는 호출자의 `decrypt` 권한과 감사 정책을 따라야 합니다.

## OpenBao 클라이언트 연결 전 확인

1. 클라이언트가 호출하는 모든 `/v1/*` 경로를 목록화합니다.
2. [호환성 프로파일](compatibility.md)에 포함되어 있는지 확인합니다.
3. 테스트 환경에서 상태 코드, 필드, TTL, 오류를 비교합니다.
4. 지원하지 않는 경로가 있으면 `/api/v1/*`로 자동 대체하지 않습니다.
5. 쓰기 요청은 백업과 감사 확인 후 운영에 적용합니다.
