# OpenBao 호환성 프로파일

## 결론

jikim `v0.2.1`은 **OpenBao 전체 호환 제품이라고 주장하지 않습니다.** `/v1/*`는 확장 기능을 넣는 경로가 아니라 호환성 계약을 위한 경계입니다. 아래 표는 현재 핸들러가 구현한 범위와 알려진 제약의 상한을 표시합니다.

기준으로 삼을 문서·향후 비교 대상은 OpenBao `2.6.1` 계열입니다. `v0.2.1`에는 동일한 요청을 OpenBao와 jikim에 보내 상태 코드·JSON schema·TTL·lease·오류 의미를 비교하는 **differential compatibility suite가 구현되지 않았습니다.** 따라서 이 버전의 표에는 `검증` 상태가 없으며, 제한 프리뷰를 무수정 호환성 보장으로 해석하면 안 됩니다.

## 상태 정의

| 상태 | 의미 |
| --- | --- |
| 검증 | OpenBao와의 differential 테스트에서 정의한 요청·응답·상태 계약 통과. v0.2.1 해당 없음 |
| 제한 | 일부 동작만 구현, 문서에 적힌 제약 내에서 사용 |
| 프리뷰 | 실험적 구현, 기존 클라이언트 운영 연결 비권장 |
| 미지원 | 구현·검증되지 않음 |

## v0.2.1 프로파일

| 영역 | 경로 예시 | 상태 | 범위와 제약 |
| --- | --- | --- | --- |
| 시스템 상태 | `GET /v1/sys/health` | 제한 | 서비스 상태 응답의 일부 필드. Seal/HA 의미 전체를 보장하지 않음 |
| KV v2 | `/v1/secret/data/*`, `/v1/secret/{delete,undelete,destroy}/*`, `/v1/secret/metadata/*` | 제한 | 고정 `secret` mount의 data, CAS, soft-delete, undelete, destroy, metadata 조회·목록·전체 삭제만 지원. 아래 세부 계약 참조 |
| Userpass 로그인 | `/v1/auth/userpass/login/:username` | 제한 | 로컬 사용자 인증과 설정된 session timeout의 비갱신 토큰 발급. 기본 12시간 |
| Token | `/v1/auth/token/{lookup-self,create,revoke-self}` | 제한 | self lookup, 관리자·매니저의 동일 사용자 token 생성, self revoke만. create TTL 최대 30일, parent/renew/wrap 의미 미지원 |
| Transit | `/v1/transit/encrypt/*`, `/v1/transit/decrypt/*` | 제한 | 이름 있는 AES-256-GCM 키의 단일 base64 암·복호화만. batch, rewrap, sign, HMAC, key 설정·목록 API 미구현 |
| Policy | `/v1/sys/policies/acl/*` | 미지원 | 정책 관리는 jikim `/api/v1/policies`에서 제공하며 OpenBao 계약이 아님 |
| Namespace 헤더 | `X-Vault-Namespace` | 미지원 | 멀티테넌시 의미를 제공한다고 가정하지 않음 |
| Seal / Unseal | `/v1/sys/seal*` | 미지원 | `ENCRYPTION_KEY` 부트스트랩 프로파일 사용 |
| Lease / Renewal | `/v1/sys/leases/*` | 미지원 | 동적 자격증명 lease 의미 미제공 |
| PKI | `/v1/pki/*` | 미지원 | 확장 단계 |
| Database 동적 자격증명 | `/v1/database/*` | 미지원 | 확장 단계 |
| AppRole·LDAP·Kubernetes Auth | `/v1/auth/*` | 미지원 | v0.2.1은 로컬 로그인과 관리 OIDC 중심 |
| Raft / HA | `/v1/sys/storage/raft/*` | 미지원 | v0.2.1 저장소는 PostgreSQL |
| Agent / Plugin API | 관련 API | 미지원 | 확장 단계 |

이 표는 구현 범위의 상한입니다. 현재 자동 테스트는 jikim 내부 계약과 보안 경계를 확인하며 OpenBao 2.6.1과의 동일 입력 비교를 수행하지 않습니다. 운영 연결 전에는 사용 호출을 대상으로 별도 호환성 테스트를 수행하십시오.

## KV v2 제한 계약

### Mount와 공통 경계

- mount 이름은 `secret`으로 고정됩니다. `sys/mounts`, mount tune, mount별 `max_versions`, `cas_required`, `delete_version_after` 설정은 구현하지 않습니다.
- 인증에는 `X-Vault-Token`, Bearer 또는 jikim 세션 쿠키로 확인 가능한 jikim 세션을 사용합니다. OpenBao token/policy 저장 모델을 그대로 구현한 것이 아닙니다.
- 권한은 jikim capability로 판정합니다. data 읽기와 metadata 조회는 `read`, 목록은 `list`, 새 key는 `create`, 기존 key 쓰기와 버전 delete/undelete/destroy는 `update`, 최신 버전 및 전체 metadata 삭제는 `delete`가 필요합니다.
- 응답은 OpenBao형 `request_id`, `data` 또는 `auth`, `errors` envelope의 구현된 필드를 반환하지만 lease, wrap, mount 의미는 완전하지 않습니다.

### Data 읽기와 쓰기

| Method와 path | 구현 동작 | 알려진 제한 |
| --- | --- | --- |
| `GET /v1/secret/data/{path}` | 현재 버전의 `data`와 version metadata 반환 | 현재 버전이 soft-delete 상태여도 이전 활성 버전으로 자동 후퇴하지 않고 `404` |
| `GET /v1/secret/data/{path}?version=N` | 양의 정수 `N` 버전 조회 | 없는·soft-delete·destroy 버전은 `404`; 빈 값과 0 이하 값은 최신으로 취급, 정수가 아니면 `400` |
| `POST`·`PUT /v1/secret/data/{path}` | `data` map으로 새 버전 생성 | JSON Merge Patch, subkeys, metadata write, mount-level CAS 설정 없음 |

쓰기 본문은 다음 하위 집합입니다.

```json
{
  "data": {
    "username": "app",
    "password": "REDACTED"
  },
  "options": {
    "cas": 3
  }
}
```

`data` 필드는 필수지만 빈 object는 허용합니다. `options.cas` 의미는 다음과 같습니다.

- 생략: current version과 무관하게 새 버전 생성
- `0`: 해당 key가 없을 때만 생성
- 양수 `N`: 현재 버전이 정확히 `N`일 때만 새 버전 생성
- 불일치 또는 CAS 동시 쓰기 경쟁 패자: `400`과 `{"errors":[...]}`

CAS 검사는 PostgreSQL serializable transaction과 행 잠금 안에서 버전 생성과 함께 수행됩니다. 이것은 jikim 내부 동시성 테스트가 확인하는 계약이며 OpenBao 2.6.1 differential 검증을 의미하지 않습니다. 기존 key를 `/v1`에서 갱신할 때 jikim의 owner, application, description, tags와 기존 version metadata는 유지됩니다.

### 버전 삭제·복구·파괴

| Method와 path | Body | 구현 동작 |
| --- | --- | --- |
| `DELETE /v1/secret/data/{path}` | 없음 | current version에 `deletion_time`을 기록하는 soft-delete |
| `POST /v1/secret/delete/{path}` | `{"versions":[1,2]}` | 지정한 미파괴 버전 soft-delete |
| `POST /v1/secret/undelete/{path}` | `{"versions":[1,2]}` | 지정한 soft-delete 버전의 `deletion_time` 제거 |
| `PUT /v1/secret/destroy/{path}` | `{"versions":[1,2]}` | 지정 버전의 암호문·nonce를 비우고 `destroyed=true`로 영구 표시 |

성공 응답은 `204 No Content`입니다. `versions` 배열 자체가 비어 있으면 `400`이지만, 존재하지 않는 버전과 0 이하 버전은 개별 오류 없이 무시될 수 있습니다. 호출자는 후속 metadata 조회로 실제 상태를 확인해야 합니다. Destroy된 버전은 undelete할 수 없고 data를 반환하지 않습니다.

### Metadata와 LIST

| Method와 path | 구현 동작 |
| --- | --- |
| `GET /v1/secret/metadata/{path}` | current/oldest version, 생성·수정 시각, current custom metadata, 모든 버전의 생성·삭제·파괴 상태 반환 |
| `LIST /v1/secret/metadata/{prefix}` | prefix 바로 아래 key와 `/` 접미사의 폴더 이름 반환 |
| `GET /v1/secret/metadata/{prefix}?list=true` | 위 LIST의 HTTP GET fallback. `list=1`도 지원 |
| `DELETE /v1/secret/metadata/{path}` | key와 모든 version row를 영구 삭제 |

Metadata 응답에서 `cas_required=false`, `metadata_cas_required=false`, `max_versions=0`, `delete_version_after="0s"`, `current_metadata_version=0`, `oldest_version=0`은 현재 구현의 고정값입니다. 자동 버전 pruning, metadata 갱신·CAS, pagination은 없습니다. LIST 결과가 비어 있으면 `404`입니다. 전체 metadata 삭제와 존재하지 않는 key의 일부 삭제 요청은 `204`가 될 수 있으므로 삭제 후 조회 검증이 필요합니다.

Unit test는 wire field, LIST fallback, CAS 검증과 오류 envelope를 확인합니다. PostgreSQL을 포함한 CAS·delete·undelete·destroy lifecycle 검증은 `JIKIM_TEST_POSTGRES_DSN`을 지정한 opt-in integration test입니다. 어느 테스트도 기준 OpenBao 인스턴스와 자동 비교하지 않습니다.

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
