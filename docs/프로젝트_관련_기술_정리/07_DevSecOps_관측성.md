# DevSecOps·관측성

## 학습 항목

- multi-stage image, non-root, read-only filesystem
- SBOM, SAST, dependency·container·secret scan
- image signing과 admission policy
- Docker Compose health check, profile, network, volume, resource 제한
- OpenTelemetry context propagation과 SLI/SLO
- backup/restore, canary, rollback rehearsal

## 프로젝트 핵심 SLI

수신 지연, queue lag, 신선한 기체 비율, 명령 dispatch·결과 지연, 미확인 명령 수, coverage 지연, incident 처리시간, AI fallback 비율을 측정한다.

보안 검사는 파이프라인 통과 장식이 아니라 릴리스 차단 조건과 예외 승인 기록을 가진다.
