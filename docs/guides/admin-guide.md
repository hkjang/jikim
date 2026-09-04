# 관리자 가이드

이 문서는 jikim `v0.2.1` 서비스 관리자를 위한 운영 절차입니다. OS·컨테이너 부트스트랩과 일상적인 서비스 설정을 분리합니다.

## 1. 관리 경계

컨테이너에는 다음 네 값만 전달합니다.

| 환경변수 | 용도 | 주의사항 |
| --- | --- | --- |
| `POSTGRES_DSN` | 메타데이터 및 암호문 저장소 연결 | TLS와 최소 권한 DB 계정 권장 |
| `BOOTSTRAP_ADMIN` | 최초 관리자 아이디 | 3~128자, 개인 실명 계정보다 초기화 전용 계정 권장 |
| `BOOTSTRAP_ADMIN_PASSWORD` | 최초 관리자 비밀번호 | 12자 이상, Secret 파일이나 안전한 주입 수단 사용 |
| `ENCRYPTION_KEY` | AES-256-GCM 마스터 암호화 키 | 32바이트 원문, 64자리 hex 또는 32바이트 base64 |

Keycloak, AI, 승인, 정책, 서명 Webhook 같은 일상 설정은 환경변수를 늘리지 않고 **서비스 관리 → 시스템 설정**에서 관리하는 것이 제품 계약입니다.

## 2. 최초 로그인

1. `/readyz`가 성공한 뒤 `http://서비스주소:8080/login`으로 이동합니다.
2. `BOOTSTRAP_ADMIN`과 `BOOTSTRAP_ADMIN_PASSWORD`로 로그인합니다.
3. 개인 관리자 계정을 만들거나 OIDC 관리자 그룹을 매핑합니다.
4. 초기화 전용 계정의 비밀번호를 회전하고 사용 범위를 제한합니다.
5. 화면 하단의 버전이 배포한 이미지 태그와 같은지 확인합니다.

부트스트랩 값은 기존 관리자의 암호를 매 기동마다 덮어쓰기 위한 수단이 아닙니다. 복구 절차를 검증하기 전 컨테이너 환경에서 임의로 바꾸지 마십시오.

## 3. 서비스 관리자와 개인화 영역

- **서비스 관리**: OIDC, AI, 승인, 전역 보안 정책, 사용자와 역할, 감사 보존 등 조직 전체 설정
- **개인화**: 내 프로필, 내 세션, 개인 키, 개인 키 회전과 허용 작업

역할은 `admin`, `manager`, `user`, `auditor`를 기본 프로파일로 사용합니다. 실제 권한은 역할 이름만 믿지 말고 정책 평가 결과와 감사 로그로 검증하십시오.

## 4. Keycloak OIDC 연결

### Keycloak 준비

1. 전용 Realm 또는 기존 보안 Realm에 OIDC Client를 만듭니다.
2. Client authentication을 활성화하고 Authorization Code Flow를 사용합니다.
3. 관리 화면에 표시되는 절대 Backend Callback URL을 Keycloak의 Valid redirect URIs에 정확히 등록합니다. 형식은 `https://<서비스 Origin>/api/v1/oidc/callback`이며 프런트엔드 경로 `/oidc/callback`과 혼동하지 않습니다.
4. Web origins는 서비스의 실제 HTTPS Origin으로 제한합니다. 와일드카드는 사용하지 않습니다.
5. 그룹 또는 역할 claim을 토큰에 포함하되 최소 정보만 전달합니다.

### jikim 설정

**서비스 관리 → 시스템 설정 → OIDC / Keycloak**에서 다음 값을 입력합니다.

- Issuer URL: `https://keycloak.example/realms/<realm>`
- Client ID
- Client Secret
- Scopes: 일반적으로 `openid profile email groups`
- 사용자명 claim 이름
- Group claim 및 Role claim 이름
- Redirect URL: 외부 사용자가 접근하는 서비스 Origin 기준의 절대 URL `https://<서비스 Origin>/api/v1/oidc/callback`

Issuer의 `/.well-known/openid-configuration`을 통해 Authorization, Token, JWKS URL, UserInfo, End Session Endpoint 메타데이터를 발견합니다. 관리 화면의 **Discovery 연결 테스트**는 다음 범위만 확인합니다.

- TLS 인증서 체인을 jikim 컨테이너가 신뢰하는지
- Issuer가 정확히 일치하는지
- Discovery 문서를 읽고 표시할 엔드포인트 메타데이터가 있는지

이 테스트는 Client ID·Client Secret의 실제 인증, JWKS 키 다운로드와 ID Token 서명 검증, Keycloak에 등록된 Redirect URI, claim·역할 매핑을 검증하지 않습니다. 저장 후 전용 테스트 계정으로 실제 SSO 로그인을 완료하고 Redirect, 서명, 사용자명·그룹·역할 매핑을 확인하십시오.

브라우저 로그인 시작 시 프런트엔드는 같은 Origin의 `/oidc/callback`을 복귀 화면으로 요청합니다. 공급자는 위의 Backend Callback으로 돌아오고, jikim은 state·nonce·PKCE와 ID Token을 검증한 뒤 짧은 TTL의 일회용 `code`만 프런트엔드에 전달합니다. 프런트엔드는 `POST /api/v1/oidc/exchange`로 이를 교환하며 세션은 응답 JSON이 아니라 HttpOnly 쿠키로 설정됩니다. 프록시는 원래 `Host`와 `X-Forwarded-Proto`를 정확히 전달해야 합니다.

Client Secret은 민감 설정으로 저장하고 화면·감사 로그·AI 요청에 평문을 남기지 않습니다. 비상 로컬 로그인을 검증하기 전 OIDC만을 유일한 관리 경로로 만들지 마십시오.

## 5. 승인 워크플로

기본값은 비활성입니다. 비활성일 때 생성·변경 작업에 임의의 검토/승인/반려 단계를 삽입하지 않습니다.

v0.2.1에서 설정할 수 있는 항목은 다음과 같습니다.

- 적용 작업: Secret 생성·변경 및 Secret 폐기
- 검토 역할: `manager` 또는 `admin`
- 워크플로 활성화 여부

승인은 1인 검토 방식이며 요청자 본인 승인은 항상 금지됩니다. 환경·위험 등급별 조건, 다단계 승인, 요청 만료, 키 회전·정책 변경 승인은 후속 범위입니다. 저장 후 일반 사용자와 검토자 계정으로 각각 한 번 테스트하십시오. 요청자는 자기 요청을 승인할 수 없어야 하며 승인·반려 결과가 감사 로그에 남아야 합니다.

## 6. AI 설정

AI는 기본 비활성입니다. 활성화 시 관리 화면에서 OpenAI-compatible Base URL, 모델, 인증 방식, 인증 정보, timeout과 `max_tokens`를 설정합니다. Base URL은 서버에서 `/v1/chat/completions` 형태로 정규화됩니다.

인증 방식은 다음 셋 중 하나입니다.

| `auth_type` | Upstream 요청 |
| --- | --- |
| `bearer` | `Authorization: Bearer <API key>` |
| `api-key` | `api-key: <API key>` |
| `none` | 인증 헤더 없음 |

API Key는 민감 설정으로 암호화 저장되며 설정 조회 응답에는 평문 대신 설정 여부만 표시됩니다. `none`이 아닌 인증 방식을 활성화하려면 API Key를 먼저 저장합니다.

- 응답은 스트리밍을 기본으로 처리합니다.
- `max_tokens`는 서비스 상한 `262144` 이내에서 모델 실제 상한보다 작게 지정합니다.
- Secret 평문, 개인 키, Client Secret, 세션 토큰을 프롬프트에 포함하지 않습니다.
- 서버가 역할과 집계 데이터로 안전한 운영 컨텍스트를 구성하며, 클라이언트가 임의의 Secret 컨텍스트를 주입하는 방식은 사용하지 않습니다.
- 외부 인터넷 대신 승인된 내부 AI Gateway 주소를 사용합니다.
- 프롬프트와 결과의 감사·보존 정책을 사전에 정합니다.

설정을 저장한 뒤 **저장된 AI 연결 테스트**를 실행하십시오. 이 테스트는 `POST /api/v1/integrations/ai/test`에서 저장된 설정으로 `stream: true`, `max_tokens: 1` 요청을 보내고, Upstream이 HTTP 2xx, `Content-Type: text/event-stream`, 하나 이상의 `data:` 이벤트를 반환하는지 확인합니다. 단순 TCP 연결 테스트가 아닙니다.

AI 요청 제한은 사용자별 인메모리 기준으로 분당 30회, 동시 2개입니다. 초과 시 `429`와 `Retry-After: 60`을 반환합니다. 서버 재시작 시 카운터가 초기화되므로 외부 API Gateway 제한을 함께 두는 것을 권장합니다. `timeout_seconds`는 10~3600초, `max_tokens`는 1~262144 범위입니다.

모델의 256K 컨텍스트 지원과 256K 출력 지원은 다른 개념입니다. 모델 제공자의 실제 출력 한도를 확인하여 더 낮은 값을 설정하십시오.

## 7. 서명 Webhook 알림

**서비스 관리 → 시스템 설정 → 알림**에서 Webhook URL, 전송 이벤트, 서명 Secret과 내부 HTTP 허용 여부를 관리합니다. HTTPS가 기본이며, 비 loopback HTTP는 **Webhook 내부 HTTP 허용**을 명시적으로 켠 경우에만 저장할 수 있습니다. 서명 Secret은 32자 이상이어야 하며 처음 저장할 때 비어 있으면 안전한 값이 자동 생성됩니다. 회전하면 이후 전송부터 새 Secret으로 서명되므로 수신 측을 함께 전환하십시오.

지원 이벤트는 다음과 같습니다.

```text
approval.requested  approval.approved  approval.rejected
secret.created      secret.updated     secret.rotated
secret.deleted      rotation.failed
```

본문에는 `delivery_id`, `event`, `occurred_at`, `resource`, `actor_id`, `request_id`, 민감하지 않은 `data`가 포함됩니다. Secret 평문은 포함하지 않습니다. 수신 요청에는 다음 헤더가 전달됩니다.

```text
X-Jikim-Delivery: <delivery UUID>
X-Jikim-Event: <event name>
X-Jikim-Timestamp: <Unix seconds>
X-Jikim-Signature-256: sha256=<hex digest>
```

서명 입력은 `X-Jikim-Timestamp + "." + raw HTTP body`이며 HMAC-SHA256을 사용합니다. 수신기는 JSON을 다시 직렬화하기 전에 원문 body로 상수 시간 비교를 수행하고, timestamp 허용 오차와 `delivery_id` 중복을 검사해야 합니다. HTTP 2xx만 성공으로 처리하며 일반 이벤트 전송 제한 시간은 10초입니다.

저장 후 **저장된 Webhook 연결 테스트**를 실행합니다. 테스트도 `integration.test` 전달 이력으로 저장됩니다. 운영 API는 다음과 같습니다.

| 작업 | API | 역할 |
| --- | --- | --- |
| 서명된 테스트 전송 | `POST /api/v1/integrations/webhook/test` | `admin` |
| 전송 이력 | `GET /api/v1/integrations/webhook/deliveries?status=failed&limit=50&offset=0` | `admin`, `auditor` |
| 저장 payload 재전송 | `POST /api/v1/integrations/webhook/deliveries/{id}/retry` | `admin` |

이력 상태는 `pending`, `delivered`, `failed`이며 시도 횟수, 응답 상태, 최근 오류와 시각을 기록합니다. 자동 백오프 재시도는 제공하지 않습니다. 실패 건은 원인을 해소한 뒤 API로 수동 재시도하며, 재시도에는 현재 저장된 URL과 서명 Secret을 사용합니다. 동시 비동기 전송 슬롯 16개가 모두 차면 해당 이력은 실패로 기록됩니다.

## 8. 키와 권한

개인 키는 사용자별 버전으로 관리합니다. 개인 키에 `encrypt`, `decrypt`, `rotate` 작업 권한을 두며 다음 원칙을 적용합니다.

- 기존 Secret 버전을 계속 읽을 수 있도록 개인 키의 `decrypt` 권한은 제거할 수 없음
- Secret 경로별 읽기·쓰기 권한은 개인 키가 아니라 접근 정책으로 최소화
- 키 회전자는 대상 데이터와 영향 범위를 확인
- 비활성 이전 버전은 기존 암호문 복호화에 필요한 동안 보존
- 마스터 `ENCRYPTION_KEY`와 개인 키 회전을 혼동하지 않음
- 회전·권한 변경 전후 감사 로그 확인

마스터 키 변경은 단순 환경변수 교체가 아닙니다. 데이터 재암호화·롤백 절차가 제공되고 검증되기 전에는 변경하지 마십시오.

## 9. 운영 점검

- 매일: `/healthz`, `/readyz`, 로그인 실패, 승인 대기, 고위험 Secret, 실패한 Webhook 전송
- 매주: 관리자·감사자 권한, 미사용 세션, 수동 회전 기록, OIDC 연결
- 매월: 복구 테스트, 관리자 계정 검토, 감사 보존량, 인증서 만료
- 릴리스 전: `make release-check`, 버전 표시, 모바일·새로고침, 전체 화면 캡처 계약

장애 대응과 취약점 신고는 [보안 가이드](security.md) 및 저장소의 `SECURITY.md`를 따릅니다.
