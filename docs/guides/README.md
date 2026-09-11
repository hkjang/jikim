# jikim v0.2.9 가이드

jikim은 애플리케이션 중심 Secret 관리와 폐쇄망 배포를 목표로 하는 Go + React 서비스입니다. 이 문서는 **v0.2.9 구현 프로파일**을 기준으로 하며, 로드맵 기능을 현재 지원 기능처럼 설명하지 않습니다.

| 문서 | 대상 | 내용 |
| --- | --- | --- |
| [관리자 가이드](../ADMIN_GUIDE.md) | 서비스 관리자 | 구성 요소, 설치, 환경변수, 계정·권한, 운영, 장애 대응, 보안 (화면 캡처 포함, PDF 함께 제공) |
| [사용자 가이드](../USER_GUIDE.md) | 일반 사용자·팀장·감사자 | 처음 5분, 화면별 사용법, 자주 하는 작업, 막혔을 때 (화면 캡처 포함, PDF 함께 제공) |
| [API 가이드](api-guide.md) | 개발자·플랫폼 엔지니어 | 인증, 응답, 스트리밍, API/MCP 경계 |
| [오프라인 설치](offline-install.md) | 인프라 운영자 | 번들 검증, Docker 적재, 기동·백업 |
| [보안 가이드](security.md) | 보안 담당자·관리자 | 암호화 경계, 키 보호, 네트워크·감사 |
| [호환성 가이드](compatibility.md) | OpenBao 연동 담당자 | 제한 핸들러 프로파일, differential suite 미구현 상태와 제약 |

## 공통 운영 계약

- 서비스명: `jikim`
- 현재 버전: `v0.2.9`
- 고정 포트: `8080`
- 이미지: `jikim:v0.2.9`
- 오프라인 번들: `jikim-v0.2.9.tar.gz`
- 필수 애플리케이션 환경변수: `POSTGRES_DSN`, `BOOTSTRAP_ADMIN`, `BOOTSTRAP_ADMIN_PASSWORD`, `ENCRYPTION_KEY`
- 상태 확인: `GET /healthz`, `GET /readyz`
- OpenBao 호환 상태 확인: `GET /v1/sys/health` — v0.2.9 제한 프로파일

운영 전에는 GitHub Release의 체크섬, 릴리스 노트, 이 문서의 호환성 표를 함께 확인하십시오.
