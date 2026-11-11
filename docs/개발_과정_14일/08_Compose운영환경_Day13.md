# Day 13 — Docker Compose 운영환경

## 작업

- React, Go, Rust, Python, PostgreSQL/PostGIS, Redis를 Compose로 구성한다.
- `core`, `analysis`, `observability` profile을 적용한다.
- health check, 내부 network, named volume과 자원 제한을 설정한다.
- 이미지에 multi-stage build와 non-root 사용자를 적용한다.
- SBOM, secret, dependency·container scan을 실행한다.

## 완료 게이트

새 환경에서 Compose만으로 모든 완료 슬라이스를 재현하고, 노트북 자원 범위 안에서 안정적으로 실행한다.
