# OpenBao 호환성 프로파일

## 결론

jikim `v0.1.0`은 **OpenBao 전체 호환 제품이라고 주장하지 않습니다.** `/v1/*`는 확장 기능을 넣는 경로가 아니라 호환성 계약을 위한 경계입니다. 아래 표는 현재 핸들러가 구현한 범위와 알려진 제약의 상한을 표시합니다.

기준으로 삼을 문서·향후 비교 대상은 OpenBao `2.6.1` 계열입니다. `v0.1.0`에는 동일한 요청을 OpenBao와 jikim에 보내 상태 코드·JSON schema·TTL·lease·오류 의미를 비교하는 **differential compatibility suite가 구현되지 않았습니다.** 따라서 이 버전의 표에는 `검증` 상태가 없으며, 제한 프리뷰를 무수정 호환성 보장으로 해석하면 안 됩니다.

## 상태 정의

| 상태 | 의미 |
| --- | --- |
| 검증 | OpenBao와의 differential 테스트에서 정의한 요청·응답·상태 계약 통과. v0.1.0 해당 없음 |
| 제한 | 일부 동작만 구현, 문서에 적힌 제약 내에서 사용 |
| 프리뷰 | 실험적 구현, 기존 클라이언트 운영 연결 비권장 |
| 미지원 | 구현·검증되지 않음 |

## v0.1.0 프로파일

| 영역 | 경로 예시 | 상태 | 범위와 제약 |
| --- | --- | --- | --- |
| 시스템 상태 | `GET /v1/sys/health` | 제한 | 서비스 상태 응답의 일부 필드. Seal/HA 의미 전체를 보장하지 않음 |
| KV v2 | `/v1/secret/data/*`, `/v1/secret/metadata/*` | 제한 | data·metadata 기본 작업 중심. 세부 버전 삭제/복구/파괴는 엔드포인트 테스트 확인 필요 |
| Userpass 로그인 | `/v1/auth/userpass/login/:username` | 제한 | 로컬 사용자 인증과 비갱신 12시간 토큰 발급 중심 |
| Token | `/v1/auth/token/*` | 제한 | 생성·lookup·revoke의 구현된 요청만. 전체 TTL/renew/wrap 의미 미보장 |
| Transit | `/v1/transit/encrypt/*`, `/v1/transit/decrypt/*` | 제한 | AES-256-GCM 기본 암·복호화 중심. batch/rewrap/sign/HMAC 미지원 가능 |
| Policy | `/v1/sys/policies/acl/*` | 미지원 | 정책 관리는 jikim `/api/v1/policies`에서 제공하며 OpenBao 계약이 아님 |
| Namespace 헤더 | `X-Vault-Namespace` | 미지원 | 멀티테넌시 의미를 제공한다고 가정하지 않음 |
| Seal / Unseal | `/v1/sys/seal*` | 미지원 | `ENCRYPTION_KEY` 부트스트랩 프로파일 사용 |
| Lease / Renewal | `/v1/sys/leases/*` | 미지원 | 동적 자격증명 lease 의미 미제공 |
| PKI | `/v1/pki/*` | 미지원 | 확장 단계 |
| Database 동적 자격증명 | `/v1/database/*` | 미지원 | 확장 단계 |
| AppRole·LDAP·Kubernetes Auth | `/v1/auth/*` | 미지원 | v0.1.0은 로컬 로그인과 관리 OIDC 중심 |
| Raft / HA | `/v1/sys/storage/raft/*` | 미지원 | v0.1.0 저장소는 PostgreSQL |
| Agent / Plugin API | 관련 API | 미지원 | 확장 단계 |

이 표는 구현 범위의 상한입니다. 현재 자동 테스트는 jikim 내부 계약과 보안 경계를 확인하며 OpenBao 2.6.1과의 동일 입력 비교를 수행하지 않습니다. 운영 연결 전에는 사용 호출을 대상으로 별도 호환성 테스트를 수행하십시오.

## differential suite에서 비교할 항목

- HTTP method와 path
- `X-Vault-Token` 처리
- 요청 JSON 타입과 필수 필드
- 응답 JSON의 필수 필드와 민감정보 위치
- 상태 코드
- OpenBao 형식 오류 응답
- 요청 ID
- TTL, lease, renew, revoke 의미가 적용되는 경우 시간 동작
- LIST method와 query fallback
- 버전 생성, 삭제와 metadata 의미

필드가 우연히 비슷하다는 이유만으로 호환으로 표시하지 않습니다.

## 확장 API 분리

jikim 사용자·관리 기능은 `/api/v1/*`, MCP는 `/mcp`에 있습니다. 이 경로는 OpenBao 호환 계약이 아니며 `bao` CLI가 사용할 대상으로 보지 않습니다.

새 기능이 필요할 때 `/v1/*` 응답에 jikim 전용 필드를 무분별하게 추가하지 않습니다. 호환 응답을 바꿔야 한다면 먼저 비교 테스트와 마이그레이션 영향을 검토합니다.

## 기존 클라이언트 검증 절차

1. 프록시 또는 애플리케이션 로그에서 실제 사용하는 `/v1/*` 호출을 수집합니다.
2. 토큰 값과 Secret 평문을 제거한 재현 fixture를 만듭니다.
3. 같은 요청을 기준 OpenBao와 jikim 테스트 인스턴스에 보냅니다.
4. 상태 코드, JSON schema, 헤더와 상태 변화를 비교합니다.
5. 시간 관련 테스트는 허용 오차와 기준 시계를 명시합니다.
6. 차이를 문서화하고 지원되지 않는 호출이 하나라도 있으면 무수정 전환을 보류합니다.

`bao` CLI, Terraform Provider, Kubernetes Operator 또는 OpenBao SDK가 “연결된다”는 사실만으로 전체 워크플로가 호환된다고 판단하지 마십시오.

## 버그 보고에 필요한 정보

- jikim 버전, commit, 이미지 digest
- 비교 OpenBao 정확한 버전
- 민감정보를 제거한 요청 method/path/body/headers
- 양쪽 상태 코드와 응답 schema 차이
- 재현 횟수와 시간 조건
- 기대한 lease 또는 버전 상태 변화

Secret, 토큰, 개인 키, DSN은 이슈에 첨부하지 않습니다.
