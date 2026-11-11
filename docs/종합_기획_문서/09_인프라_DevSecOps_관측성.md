# 인프라·DevSecOps·관측성

## 1. 환경 구성

V01의 개발·통합시험·데모·단일 서버 배포 표준은 Docker Compose다. Go API, Rust Gateway, Python Simulator/Analysis, React UI를 독립 이미지로 빌드하고 profile과 override 파일로 실행 범위를 구분한다. PostgreSQL/PostGIS, Redis, Object Storage는 상태 저장 계층으로 분리한다. Kubernetes는 V01 범위에 포함하지 않는다.

## 2. CI/CD 게이트

| 단계 | 필수 검증 |
|---|---|
| PR | lint, unit test, contract test, secret scan |
| Build | SBOM, dependency·container scan, image signature |
| Integration | replay, migration, 권한·동시성 시험 |
| Staging | 부하, 장애 주입, rollback rehearsal |
| Release | 승인된 digest와 설정만 배포 |

Go는 제품의 CI/CD 구현 언어가 아니라 서비스 구현 언어다. 파이프라인은 GitHub Actions와 선언형 배포 도구로 구성하며 별도의 Go 운영 도구는 반복 검증이 실제로 필요할 때만 만든다.

## 3. 네트워크·보안

- 외부 기체 구간, 수집 구간, 애플리케이션 구간, 데이터 구간을 분리한다.
- 서비스 간 mTLS, 최소 권한, 단기 자격증명을 적용한다.
- 명령 채널과 텔레메트리 채널을 논리적으로 분리한다.
- 명령은 요청자, 승인자, 정책 버전, 통제 lease, 만료 시각을 재검증한다.
- 이미지와 의존성의 출처, 서명, SBOM을 릴리스 증거로 보존한다.

## 4. 관측성

OpenTelemetry 기반 trace·metric·log 상관관계를 유지한다. 핵심 지표는 텔레메트리 수신 지연, drop·duplicate, queue lag, API 지연, 명령 단계별 시간, coverage, incident 처리 시간, 모델 fallback이다.

운영 대시보드는 서비스 상태보다 임무 상태를 먼저 보여준다. 예를 들어 `정상 pod 수`만이 아니라 `최신 위치를 확인할 수 없는 기체 수`, `미순찰 구간`, `결과 미확인 명령`을 표시한다.

## 5. 복구 전략

단일 서버 배포는 이미지 digest가 고정된 Compose 파일과 환경별 설정을 사용한다. 새 버전은 별도 project name과 포트에서 smoke test한 뒤 reverse proxy 대상을 전환한다. 스키마는 expand-migrate-contract 순서로 변경한다. rollback은 이전 이미지 digest, 이벤트 계약과 DB 호환성을 포함해 리허설한다.
