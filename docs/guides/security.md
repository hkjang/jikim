# 보안 운영 가이드

이 문서는 jikim `v0.2.6`의 보안 경계와 운영자가 추가로 구성해야 할 통제를 설명합니다. Secret 관리 서비스 자체가 단일 실패 지점이 되지 않도록 애플리케이션, 데이터베이스, 키, 네트워크와 운영자 권한을 함께 다룹니다.

## 위협 모델과 신뢰 경계

jikim은 다음을 가정합니다.

- PostgreSQL 저장소는 평문 Secret을 신뢰할 수 있는 공간으로 보지 않습니다.
- 컨테이너 호스트와 마스터 키에 동시에 접근한 공격자는 높은 위험을 가집니다.
- 관리자 계정 탈취는 Secret 평문 접근과 정책 변경으로 이어질 수 있습니다.
- AI, OIDC, Webhook 같은 외부 연동은 별도의 데이터 유출 경계입니다.
- 감사 로그는 탐지 수단이지만 권한 통제 자체를 대체하지 않습니다.

## 저장 암호화

v0.2.6 저장 암호화는 AES-256-GCM을 사용합니다. GCM nonce와 인증 태그로 암호문 변조를 탐지하며, 사용자 키와 Secret 버전은 암호문 형태로 PostgreSQL에 저장합니다.

`ENCRYPTION_KEY` 허용 형식:

- 정확히 32바이트 원문
- 64자리 hex(32바이트)
- 32바이트를 표현한 표준 또는 URL-safe base64

키는 설정 파일, 이미지 레이어, Git, 지원 티켓 또는 셸 히스토리에 넣지 않습니다. 운영 키와 비운영 키를 분리하고 PostgreSQL 백업과 다른 통제 영역에 보관합니다.

마스터 키를 분실하면 암호문을 복구할 수 없습니다. 반대로 DB와 마스터 키가 함께 유출되면 저장 암호화의 보호 효과가 크게 줄어듭니다.

## 비밀번호와 세션

- 로컬 비밀번호는 PBKDF2-HMAC-SHA-256, 반복 310,000회와 사용자별 무작위 salt로 저장합니다.
- 최소 길이는 12자이며 운영 정책에서 더 긴 passphrase를 권장합니다.
- 로그인 오류는 사용자 존재 여부를 구분하지 않습니다.
- 웹 세션 기본 수명은 12시간입니다.
- 쿠키는 HttpOnly, SameSite=Strict이며 HTTPS 인식 시 Secure를 적용합니다.
- API 토큰은 로그, URL, 브라우저 분석 도구에 노출하지 않습니다.

Reverse Proxy가 TLS를 종료한다면 신뢰할 수 있는 Proxy만 `X-Forwarded-Proto`를 설정하도록 구성하십시오. 클라이언트가 임의로 해당 헤더를 주입할 수 있는 구조는 피합니다.

## 네트워크

권장 흐름:

```text
사용자 → TLS Load Balancer → jikim:8080 → PostgreSQL(TLS)
                                  ├→ Keycloak(선택, TLS)
                                  └→ 내부 AI Gateway(선택, TLS)
```

- 관리 UI를 인터넷에 직접 공개하지 않음
- 서비스 egress를 등록된 PostgreSQL, Keycloak, AI 주소로 제한
- PostgreSQL 보안 그룹은 jikim 워크로드만 허용
- 관리 네트워크와 일반 애플리케이션 접근 경로 분리
- 폐쇄망 DNS와 시간 동기화 보호

기본 보안 헤더는 CSP, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, 제한된 `Permissions-Policy`를 포함합니다. 상위 Proxy가 이를 약화하지 않는지 확인하십시오.

## Keycloak OIDC

- Issuer 정확히 일치
- Authorization Code Flow 사용
- state, nonce와 PKCE S256 검증
- Redirect URI와 Web Origin 최소화
- Client Secret 주기적 회전
- 서명 알고리즘과 JWKS 검증
- 관리자 역할은 전용 claim 값으로 제한
- 비상 로컬 관리자 경로 정기 점검

OIDC 활성화 시 관리자가 저장하는 `redirect_url`은 절대 `http(s)` URL이며 path가 정확히 `/api/v1/oidc/callback`이어야 합니다. 비-loopback HTTP는 `allow_insecure_http`를 명시해야 하지만, 폐쇄망에서도 가능한 한 TLS를 사용하십시오. jikim은 요청 `Host`를 사용해 OAuth callback을 만들지 않습니다. SPA callback은 상대 `/oidc/callback`만 유지하며 다른 Origin은 거부합니다. Keycloak의 Valid Redirect URI도 wildcard 대신 이 한 주소로 제한하십시오.

역할 승격은 대소문자까지 정확히 일치하는 `jikim-admin`, `jikim-manager`, `jikim-auditor`, `jikim-user`만 허용합니다. `admin`, `/org/admin`, `/org/jikim-admin` 또는 대문자 변형은 인식하지 않습니다. 여러 전용 역할이 동시에 들어오면 `user`로 낮아지므로 Keycloak mapper가 한 역할만 발행하도록 구성합니다. role/group 동기화를 켠 상태에서 인식 가능한 값이 사라져도 이전 관리자 역할을 유지하지 않습니다.

공급자 오류는 원문 설명을 SPA redirect에 다시 반영하지 않고 일반화된 상대 경로 오류로 변환합니다. `GET /api/v1/oidc/logout`은 로컬 토큰 폐기와 쿠키 제거를 먼저 수행한 다음 discovery의 `end_session_endpoint`로 이동하므로 공급자 장애가 로컬 로그아웃을 막지 않습니다. 현재 프로파일에는 ID Token hint, back-channel logout, 공급자 주도 세션 폐기 알림이 없으므로 Keycloak 세션 수명과 jikim 세션 수명을 별도로 제한하십시오.

SSO가 활성화되어도 jikim 내부 정책과 감사는 계속 적용됩니다. Keycloak 역할 이름이 있다는 이유만으로 OpenBao 또는 jikim policy가 자동 생성된다고 가정하지 마십시오.

## 권한과 승인

최소 권한 원칙에 따라 `read`, `list`, `create`, `update`, `delete`, `rotate`, `encrypt`, `decrypt`를 가능한 한 분리합니다.

승인 워크플로는 관리자가 활성화한 작업에만 적용됩니다. 비활성 상태에서 승인 절차가 있다고 가정하지 마십시오. 활성화할 때는 요청자와 승인자를 다르게 하고, 운영·고위험 대상부터 적용합니다.

MCP와 API 탐색기도 현재 사용자 권한을 그대로 적용해야 합니다. 새로운 인터페이스가 기존 정책을 우회하지 않는지 릴리스 테스트에 포함합니다.

## MCP 전송과 도구 경계

`/mcp`는 인증된 stateless POST endpoint입니다. 다음은 initialize 이후 요청의 전체 헤더 예시입니다.

```http
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2025-11-25
```

`Content-Type`과 두 Accept media type은 initialize를 포함한 모든 POST에 필요합니다. `MCP-Protocol-Version`은 `initialize` 요청에서만 생략할 수 있고, 후속 요청에서는 `2025-11-25` 또는 `2025-06-18`이어야 합니다. `GET /mcp`는 `405`이며 이 버전은 서버 발 SSE stream, MCP session ID와 resume을 제공하지 않습니다.

브라우저가 `Origin`을 보내면 요청 scheme·host와 동일해야 하며 불일치 요청은 인증 처리 전에 `403`으로 거부됩니다. Origin이 없는 비브라우저 요청은 허용되므로 네트워크 ACL과 Bearer 토큰 보호를 별도로 적용합니다. Reverse Proxy는 `Host`와 `X-Forwarded-Proto`를 신뢰된 값으로 덮어쓰고 외부 입력을 그대로 전달하지 마십시오.

도구별 보안 경계:

- `secrets.list`는 `list` capability를 SQL pagination 전에 적용하며 값은 반환하지 않습니다.
- `secrets.metadata`는 정확한 path의 `read` capability를 요구하고 값은 반환하지 않습니다.
- `access.check`는 현재 세션 사용자의 권한만 계산하며 다른 `user_id`를 받지 않습니다.
- 정책과 감사 검색은 `admin`, `manager`, `auditor`만 사용할 수 있습니다.
- `transit.encrypt/decrypt`는 path policy와 이름 있는 key의 operation 권한을 모두 확인합니다.
- `transit.decrypt`는 tool 이름과 key가 식별되는 감사를 응답 전에 저장하며, 감사 저장에 실패하면 복호화 결과를 폐기합니다.

JSON-RPC notification에는 `id`를 넣지 않으며 응답은 `202`와 빈 본문입니다. 알 수 없는 인자나 `user_id` 같은 확장 필드를 임의로 보내지 않습니다. MCP 도구 오류가 HTTP `200`의 `isError=true`로 표현될 수 있으므로 HTTP 상태만으로 성공을 판단하지 마십시오.

## AI 데이터 경계

AI 모델에 보낼 수 있는 기본 범위는 Secret ID, 마스킹된 경로, 메타데이터, 정책, 위험 점수, 집계된 감사 통계와 오류 정보입니다.

보내지 않는 데이터:

- Secret 평문
- 개인 키 또는 마스터 키
- 세션·API·OpenBao 토큰
- OIDC Client Secret
- PostgreSQL DSN의 자격증명
- 복호화된 승인 payload

내부 모델이라도 같은 원칙을 적용합니다. 프롬프트 기록, 모델 제공자 보존 정책, 관리자 변경 이력을 감사 범위에 포함하십시오.

## 감사

API, `/v1/*`, MCP 요청은 요청 ID, 사용자, 동작, 리소스, 결과, IP, User-Agent와 시각을 중심으로 기록합니다. 감사 이벤트에 요청·응답 전체 본문이나 Secret 평문을 넣지 않습니다.

OpenBao KV data 읽기와 Transit 복호화는 민감 결과를 응답하기 전에 별도 disclosure 감사를 저장합니다. 해당 감사 저장이 실패하면 API는 Secret 또는 평문을 내보내지 않습니다. MCP Transit 복호화도 일반 `/mcp` 요청 감사 외에 tool/key가 들어간 이벤트를 별도로 남깁니다. 감사 DB 가용성 장애가 민감 조회 장애로 이어지는 것은 의도한 fail-closed 동작입니다.

운영자는 다음을 알림 또는 정기 검토 대상으로 삼습니다.

- 반복 로그인 실패
- 관리자·정책 변경
- Secret 조회량 급증
- 개인 키 및 Transit 키 회전
- 승인·반려와 자기 승인 시도
- AI와 MCP 사용
- 감사 저장 실패

감사자는 서비스 관리자와 분리하고 PostgreSQL 감사 테이블 접근도 최소화합니다.

## 컨테이너

제공 Compose 프로파일은 non-root 사용자, read-only root filesystem, 모든 Linux capability 제거, `no-new-privileges`, `/tmp` tmpfs를 사용합니다.

운영 추가 권장사항:

- 이미지 digest와 릴리스 SHA-256 기록
- 허용된 Registry 또는 오프라인 저장소에서만 적재
- 컨테이너 Runtime 기본 seccomp 유지
- 호스트 Docker socket을 컨테이너에 마운트하지 않음
- 이미지 SBOM·취약점 검사와 서명 절차 도입

v0.2.6 릴리스 자동화는 이미지 번들과 SHA-256을 생성하지만, 서명·SBOM·취약점 검사 도구가 별도 공급망 절차 없이 자동 제공된다고 가정하지 마십시오.

## 백업과 사고 대응

백업은 PostgreSQL과 마스터 키를 모두 포함하되 서로 분리합니다. 복구 시험에서 같은 버전 이미지, DB 복구, 키 주입, 준비 상태, 로그인과 Secret 복호화를 확인합니다.

의심되는 노출 시:

1. 관련 토큰과 세션 폐기
2. 네트워크 접근 차단 또는 읽기 전용 운영 전환
3. 감사 로그와 요청 ID 보존
4. 영향 Secret과 키, 연결 시스템 식별
5. 대상 시스템 → jikim 순서로 안전하게 자격증명 회전
6. 복구 후 정책과 승인 경계 재검증

공개 취약점 신고는 저장소의 [`SECURITY.md`](../../SECURITY.md)를 따릅니다.
