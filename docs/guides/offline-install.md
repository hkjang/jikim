# 오프라인 설치 가이드

이 절차는 `jikim:v0.2.7` 서비스 이미지를 `jikim-v0.2.7.tar.gz`로 반입하는 단일 노드 Docker 배포 프로파일입니다. PostgreSQL 설치와 백업은 운영 조직의 표준 절차를 따릅니다.

## 준비 사항

- Linux amd64 Docker 호스트
- Docker Engine 및 Docker Compose v2
- 서비스에서 접근 가능한 PostgreSQL
- 호스트 기준 최소 권장치: 2 CPU, 4 GiB RAM, 이미지·로그 여유 공간
- 시간 동기화, 내부 DNS, TLS 종료 프록시
- 네 개의 부트스트랩 값 전달 수단

릴리스 번들에는 PostgreSQL 이미지나 데이터베이스가 포함되지 않습니다. 사내 표준 PostgreSQL을 별도로 준비하십시오.

## 1. 연결망에서 릴리스 확보

GitHub Release에서 다음 두 파일을 같은 디렉터리에 받습니다.

```text
jikim-v0.2.7.tar.gz
jikim-v0.2.7.tar.gz.sha256
```

체크섬을 먼저 검증합니다.

```bash
sha256sum --check jikim-v0.2.7.tar.gz.sha256
gzip --test jikim-v0.2.7.tar.gz
```

성공 결과와 릴리스 URL, 반입 담당자, 시각을 반입 기록에 남깁니다.

## 2. 폐쇄망 반입 후 재검증

매체에서 복사한 뒤 동일한 체크섬 명령을 다시 실행합니다. 체크섬이 다르면 이미지를 적재하지 말고 반입 파일을 폐기한 뒤 다시 확보합니다.

```bash
sha256sum --check jikim-v0.2.7.tar.gz.sha256
docker load --input jikim-v0.2.7.tar.gz
docker image inspect jikim:v0.2.7 --format '{{ index .Config.Labels "org.opencontainers.image.version" }}'
```

마지막 명령 결과는 `v0.2.7`이어야 합니다.

## 3. PostgreSQL 준비

전용 데이터베이스와 최소 권한 계정을 만듭니다. 앱 시작 시 포함된 스키마 마이그레이션이 적용되므로 계정에는 해당 데이터베이스의 스키마·테이블 생성 및 CRUD 권한이 필요합니다.

운영 DSN은 가능하면 TLS를 강제합니다.

```text
postgres://jikim_app:<password>@postgres.internal:5432/jikim?sslmode=verify-full
```

컨테이너 CA 저장소가 내부 PostgreSQL 인증서 발급자를 신뢰하도록 조직 표준 방식으로 이미지를 검토하거나 TLS 프록시를 구성하십시오. `sslmode=disable`은 로컬 검증에만 사용합니다.

## 4. 암호화 키 생성

32바이트 키를 64자리 hex로 만들 수 있습니다.

```bash
openssl rand -hex 32
```

키를 `.env` 파일에 평문으로 장기 보관하지 말고 사내 Secret 전달 수단을 사용합니다. 이 값을 잃으면 저장된 암호문을 복구할 수 없습니다. 데이터 재암호화 절차 없이 값을 바꾸면 기존 데이터를 읽을 수 없습니다.

## 5. Compose 기동

저장소의 `docker-compose.yml`은 애플리케이션에 다음 네 환경변수만 전달합니다.

```bash
export POSTGRES_DSN='postgres://jikim_app:REDACTED@postgres.internal:5432/jikim?sslmode=verify-full'
export BOOTSTRAP_ADMIN='bootstrap-admin'
export BOOTSTRAP_ADMIN_PASSWORD='REDACTED-AT-LEAST-12-CHARS'
export ENCRYPTION_KEY='REDACTED-64-HEX-CHARS'
docker compose config --quiet
docker compose up --detach
```

셸 히스토리와 프로세스 목록 노출을 피하려면 운영 환경의 안전한 환경 주입 방식을 사용하십시오. 위 코드는 변수 이름을 설명하기 위한 예시입니다.

## 6. 내부 CA 고정 경로 연결

내부 CA로 서명된 Keycloak, AI Gateway 또는 Webhook endpoint를 호출해야 한다면 CA 인증서 또는 PEM bundle을 Compose 디렉터리의 `certs/internal-ca.crt`에 배치합니다. 개인 키는 이 파일에 넣지 마십시오.

```bash
install --directory --mode 0755 certs
install --mode 0444 /안전한/반입경로/internal-ca.crt certs/internal-ca.crt
docker compose -f docker-compose.yml -f docker-compose.internal-ca.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.internal-ca.yml up --detach
```

`docker-compose.internal-ca.yml`은 이 파일을 컨테이너의 고정 경로 `/app/certs/internal-ca.crt`에 read-only로 mount합니다. jikim은 시작할 때 해당 PEM을 시스템 신뢰 저장소에 추가하여 OIDC Discovery·Token/JWKS, AI, Webhook의 outbound HTTPS에 사용합니다. 파일 교체 후에는 컨테이너를 재시작하고 각 관리 화면의 연결 테스트를 다시 실행합니다.

이 mount는 PostgreSQL TLS 설정을 대체하지 않습니다. PostgreSQL CA 신뢰는 DSN과 조직의 PostgreSQL/컨테이너 표준에 따라 별도로 구성하십시오. 인증서 검증을 끄거나 네 환경변수 외의 임의 CA 환경변수를 추가하는 방식은 배포 계약이 아닙니다.

## 7. 상태 확인

```bash
curl --fail --silent http://127.0.0.1:8080/healthz
curl --fail --silent http://127.0.0.1:8080/readyz
docker compose ps
docker compose logs --tail 100 jikim
```

- `/healthz`: 프로세스가 요청을 처리할 수 있는지 확인
- `/readyz`: PostgreSQL 등 필수 의존성을 포함한 준비 상태 확인
- `/v1/sys/health`: OpenBao 제한 호환 프로파일 상태 응답

서비스 개방 전 로그인 화면과 프로필 메뉴의 버전이 `v0.2.7`인지 확인합니다.

## 8. TLS와 네트워크

이미지는 8080 HTTP 포트를 제공합니다. 운영에서는 내부 Load Balancer 또는 Reverse Proxy에서 TLS를 종료하고 다음을 제한합니다.

- 사용자 네트워크 → 서비스 8080 또는 TLS 프록시
- 서비스 → PostgreSQL
- 서비스 → 관리자가 설정한 Keycloak, AI, Webhook 등 내부 대상
- 그 외 외부 인터넷 egress 차단

관리 콘솔을 인터넷에 직접 노출하지 마십시오.

Keycloak에는 외부 서비스 Origin 기준의 절대 callback `https://<서비스 Origin>/api/v1/oidc/callback`을 정확히 등록합니다. 프런트엔드 SPA 경로 `/oidc/callback`을 Keycloak callback으로 등록하지 마십시오. TLS 종료 프록시는 원래 `Host`와 `X-Forwarded-Proto`를 보존해야 같은 Origin 검증이 정상 동작합니다.

Webhook 수신기는 jikim에서 접근 가능한 내부 주소에 두고 HTTP 2xx를 10초 안에 반환하도록 구성합니다. 수신 측 시계를 동기화하고 `X-Jikim-Timestamp`, `X-Jikim-Delivery`, `X-Jikim-Signature-256`을 검증합니다. AI Gateway는 OpenAI-compatible Chat Completions SSE를 제공해야 하며 연결 테스트에서도 `text/event-stream`과 `data:` 이벤트를 반환해야 합니다.

## 9. 백업과 복구

서비스 상태의 원본은 PostgreSQL 암호문과 마스터 `ENCRYPTION_KEY`입니다. 둘을 서로 다른 통제 영역에 백업합니다.

1. PostgreSQL 일관 백업 수행
2. 키 관리 시스템에서 마스터 키 별도 백업
3. 복구 격리망에 같은 버전 이미지 적재
4. DB 복구 후 같은 키로 `/readyz`와 로그인 검증
5. Secret 샘플 복호화 및 감사 이벤트 확인
6. OIDC, AI SSE, 서명 Webhook 테스트와 Webhook 전송 이력 조회

백업만 생성하고 복구를 시험하지 않은 상태는 복구 가능 상태가 아닙니다.

## 10. 업그레이드와 롤백

1. 새 번들의 체크섬과 호환성 프로파일 확인
2. DB 백업 및 복구 지점 기록
3. 별도 환경에서 마이그레이션과 주요 API 테스트
4. 기존 이미지 태그를 유지한 채 새 태그 적재
5. 유지보수 시간에 컨테이너 교체

DB 마이그레이션이 하위 호환인지 확인하기 전에는 단순 이미지 교체로 롤백할 수 있다고 가정하지 마십시오.
