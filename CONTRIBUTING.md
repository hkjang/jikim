# jikim에 기여하기

jikim은 한국어 우선 관리 경험, 폐쇄망 배포와 검증 가능한 OpenBao 호환 프로파일을 목표로 합니다. 기능 수보다 보안 경계와 재현 가능한 동작을 우선합니다.

## 시작 전 원칙

- Secret, 토큰, DSN, 개인 키, OIDC Client Secret을 이슈·커밋·테스트 fixture에 넣지 않습니다.
- `/v1/*`는 OpenBao 호환 계약입니다. jikim 고유 기능은 `/api/v1/*` 또는 명시된 확장 경로에 둡니다.
- 호환성을 백분율로 추측하지 않습니다. 엔드포인트별 비교 테스트 결과를 문서화합니다.
- 애플리케이션 환경변수는 `POSTGRES_DSN`, `BOOTSTRAP_ADMIN`, `BOOTSTRAP_ADMIN_PASSWORD`, `ENCRYPTION_KEY` 네 개로 유지합니다.
- 사용자 화면과 서비스 관리자 화면의 권한 경계를 유지합니다.
- 승인 워크플로가 꺼져 있으면 임의의 검토·승인 단계를 만들지 않습니다.
- 기능, 버튼, 오류 메시지는 한국어를 기본으로 하되 API의 안정적인 코드 값은 유지합니다.

## 개발 환경

- Go: `go.mod`에 선언된 버전 이상
- Node.js 24 및 npm
- Docker Engine과 Docker Compose v2
- PostgreSQL 17은 CI 스모크 테스트 프로파일이며, 다른 지원 버전은 테스트 결과로 명시

의존성을 설치합니다.

```bash
go mod download
npm --prefix web ci
```

로컬 서비스 실행에는 네 환경변수가 필요합니다. 운영 자격증명을 사용하지 마십시오.

```bash
export POSTGRES_DSN='postgres://jikim:jikim@127.0.0.1:5432/jikim?sslmode=disable'
export BOOTSTRAP_ADMIN='local-admin'
export BOOTSTRAP_ADMIN_PASSWORD='local-only-password'
export ENCRYPTION_KEY='0123456789abcdef0123456789abcdef'
go run ./cmd/server
```

## 검증

빠른 전체 검증:

```bash
./scripts/verify.sh
```

Docker 이미지까지:

```bash
./scripts/verify.sh --docker
```

PostgreSQL 이미지를 미리 준비한 뒤 외부 통신 차단 네트워크 스모크 테스트까지:

```bash
docker pull postgres:17-alpine
./scripts/verify.sh --smoke
```

실제 릴리스 전 전체 계약:

```bash
make release-check
```

`release-check`는 소스 테스트, `jikim:v0.2.5` 이미지, 상태 endpoint, 외부 egress 차단, `jikim-v0.2.5.tar.gz`와 SHA-256을 검증합니다. 릴리스 생성이나 push는 하지 않습니다.

## Go 변경

- 표준 `gofmt` 사용
- 요청 context와 timeout 전달
- 오류에 Secret 평문 포함 금지
- 암호화에서 nonce 재사용 금지
- SQL은 parameter binding 사용
- 권한 검사를 handler와 store 경계에서 명확히 유지
- API 변경에는 성공, 권한 거부, 잘못된 입력과 감사 테스트 추가

## React 변경

- 기본 본문 글자 16px 이상, 중요한 화면은 17px 수준의 가독성 유지
- 키보드만으로 메뉴·대화상자·폼 사용 가능
- 모바일 390px와 데스크톱 1440px 확인
- 라우트 기반 메뉴를 사용하여 새로고침 후 현재 화면 유지
- 로딩, 빈 결과, 오류 상태를 구분
- Secret은 기본 마스킹하고 명시적 동작에서만 표시
- 프로필 메뉴 스크롤바는 메뉴 내부에 제한
- 로그인과 프로필 컨텍스트 메뉴의 버전 일치 확인

UI 변경 시 `docs/screenshots/manifest.json`의 대상 화면을 확인하고 릴리스 후보 캡처를 갱신합니다.

## API 호환성 변경

OpenBao 호환 endpoint를 변경할 때 PR에 다음을 포함합니다.

1. 기준 OpenBao 버전
2. 요청 fixture(민감정보 제거)
3. 상태 코드와 schema 비교
4. TTL·lease·버전 상태 변화 비교
5. 알려진 차이와 마이그레이션 영향
6. `docs/guides/compatibility.md` 갱신

jikim 관리 API가 비슷하다는 이유로 `/v1/*` handler에서 재사용하지 말고 두 계약의 응답 형식을 분리합니다.

## Pull Request

- 한 PR에 하나의 명확한 목적
- 사용자 동작이 바뀌면 한국어 가이드 갱신
- DB 변경은 순방향 migration과 복구 영향을 설명
- 새 설정은 가능한 한 관리자 UI에 추가하고 애플리케이션 환경변수를 늘리지 않음
- 보안 관련 수정은 공개 PR 전에 `SECURITY.md` 절차 사용

커밋 메시지는 무엇보다 **왜** 바꾸는지 드러내야 합니다.

## 릴리스

현재 소스 버전은 `scripts/version.sh`가 단일 기준입니다. 버전 변경 PR은 Docker 기본값, 문서 프로파일과 화면 버전을 함께 갱신해야 합니다.

태그 push 후 GitHub Actions가 다음을 수행합니다.

1. 태그와 소스 버전 일치 확인
2. linux/amd64 이미지 빌드
3. 내부 Docker 네트워크에서 PostgreSQL 연동과 egress 차단 스모크 테스트
4. Docker image archive와 SHA-256 생성
5. 정확한 이미지 태그 확인
6. GitHub Release에 `tar.gz`와 체크섬만 게시

릴리스 수행 권한은 maintainer에게 있으며, 기여자는 태그나 릴리스를 임의로 생성하지 않습니다.
