# Day 11 — Replay & Audit 슬라이스

## 프론트엔드

- 사건 시점의 telemetry, 명령, 임무와 운영자 조작을 타임라인으로 표시한다.
- 확정된 순서와 시계 오차로 불확실한 순서를 구분한다.

## 백엔드

- Go에서 감사 이벤트와 replay 조회 API를 제공한다.
- 원본 이벤트로 Operational Picture projection을 재생성한다.
- projection checksum과 화면 상태를 비교한다.

## 완료 게이트

대표 장애를 사후 재현하고, 당시 확인 가능했던 사실과 이후 알게 된 정보를 구분할 수 있다.
