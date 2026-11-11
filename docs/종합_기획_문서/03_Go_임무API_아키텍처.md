# Go 임무 API 아키텍처

## 1. 책임

Go Mission API는 다음 상태의 유일한 쓰기 주체다.

- Patrol Mission·Route·Checkpoint·Coverage
- Asset·Assignment·Operation Session
- Command·Approval·Effect Verification
- Control Lease·Operator Handover
- Incident·Alert·Response History
- Safety Policy·Geofence·Deployment Metadata

## 2. 모듈 구조

```text
mission-api/
├─ cmd/api
├─ cmd/worker
├─ internal/
│  ├─ patrolmission/
│  ├─ command/
│  ├─ controlauthority/
│  ├─ operationalpicture/
│  ├─ incident/
│  ├─ safety/
│  └─ audit/
├─ contracts/
└─ migrations/
```

각 모듈은 `domain`, `application`, `adapter`로 나누고 다른 모듈의 DB 구현을 직접 호출하지 않는다.

## 3. 핵심 Aggregate

| Aggregate | 불변조건 |
|---|---|
| PatrolMission | 승인된 경로·구역·정책 없이 시작 불가 |
| Assignment | 같은 시간대 기체 중복 배정 금지 |
| Command | 허용된 상태 전이와 만료·멱등성 보장 |
| ControlLease | 기체당 활성 통제권 1개 |
| Incident | 근거 Event와 영향 구역 없이 확정 금지 |

## 4. 동시성·정합성

- 명령 상태 전이는 낙관적 잠금과 Version을 사용한다.
- HTTP Mutation은 Idempotency Key를 지원한다.
- 업무 상태 변경과 Outbox Event를 한 Transaction에 저장한다.
- Goroutine은 Request Context 취소와 상한을 가진다.
- DB Connection Pool, Worker 수와 Queue 용량을 명시한다.

## 5. API 범위

- `/api/v1/patrol-zones`
- `/api/v1/routes`, `/checkpoints`
- `/api/v1/missions`, `/assignments`
- `/api/v1/assets/{id}/operational-picture`
- `/api/v1/commands`, `/approvals`
- `/api/v1/incidents`, `/alerts`
- `/api/v1/replays`
- `/ws/operations`
