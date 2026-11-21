# API contracts

Every schema here is validated from both sides: the Go service checks its
responses against these files in its tests, and the console parses the same
examples with its Zod schemas. Changing one side alone breaks the other side's
tests on purpose.

## Endpoints

### Operational Picture (Day 01–02)

| Method | Path | Request | Response |
| --- | --- | --- | --- |
| GET | `/api/v1/operational-picture` | — | [operational-picture.schema.json](schemas/operational-picture.schema.json) |

### Patrol Mission (Day 03–04)

| Method | Path | Request | Success | Notes |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/missions` | — | 200 [patrol-mission-list.schema.json](schemas/patrol-mission-list.schema.json) | Newest plan first |
| GET | `/api/v1/missions/{id}` | — | 200 [patrol-mission.schema.json](schemas/patrol-mission.schema.json) | |
| POST | `/api/v1/missions` | [create request](schemas/patrol-mission-create-request.schema.json) | 201 mission | `Idempotency-Key` header required |
| POST | `/api/v1/missions/{id}/checkpoints` | checkpoint entry of the create request | 200 mission | Draft only |
| DELETE | `/api/v1/missions/{id}/checkpoints/{checkpointId}` | — | 200 mission | Draft only |
| POST | `/api/v1/missions/{id}/assignments` | `{ "unitId": "…" }` | 200 mission | Draft only |
| DELETE | `/api/v1/missions/{id}/assignments/{unitId}` | — | 200 mission | Draft only |
| POST | `/api/v1/missions/{id}/approve` | [transition request](schemas/patrol-mission-transition-request.schema.json) | 200 mission | Freezes the plan |
| POST | `/api/v1/missions/{id}/start` | transition request | 200 mission | |
| POST | `/api/v1/missions/{id}/complete` | transition request | 200 mission | |
| POST | `/api/v1/missions/{id}/abort` | transition request | 200 mission | `reason` required |

Checkpoints and assignments are sub-resources of a mission, not standalone
ones: approval freezes them to the mission version, so they cannot be shared or
edited from elsewhere. The reasoning is recorded in
[the mission scenario](../docs/scenarios/patrol-mission.md).

## Rules every mutation follows

**Optimistic locking.** Each transition body carries `version`, the revision
the caller last read. A transition built on an older version is refused with
`STALE_VERSION` rather than overwriting the other operator's outcome. The
response always carries the new version.

**Idempotency.** `POST /api/v1/missions` requires an `Idempotency-Key` header.
Repeating a request with the same key returns the mission created the first
time instead of creating a second one.

**Refusals are contract.** Failures return
[api-error.schema.json](schemas/api-error.schema.json) with a `code` the client
keys off, and `details` carrying the facts needed to act — which unit conflicts,
which checkpoint lies outside the area, which version was expected.

## Error codes

| Code | HTTP | Meaning |
| --- | --- | --- |
| `INVALID_REQUEST` | 400 | Body does not match the request schema |
| `MISSION_NOT_FOUND` | 404 | No mission with that id |
| `CHECKPOINT_NOT_FOUND` | 404 | No checkpoint with that id on this mission |
| `PATROL_AREA_NOT_FOUND` | 404 | The plan references an unknown area |
| `UNIT_NOT_FOUND` | 404 | The assignment references an unknown unit |
| `NO_CHECKPOINTS` | 422 | Approval needs at least one checkpoint |
| `NO_ASSIGNMENT` | 422 | Approval needs at least one assigned unit |
| `INVALID_WINDOW` | 422 | The operating window ends before it starts |
| `CHECKPOINT_OUTSIDE_AREA` | 422 | A checkpoint lies outside the patrol area |
| `ASSIGNMENT_CONFLICT` | 409 | A unit is already committed to an overlapping window |
| `TRANSITION_NOT_ALLOWED` | 409 | Not a legal transition from the current state |
| `STALE_VERSION` | 409 | The mission changed since the caller read it |
| `ABORT_REASON_REQUIRED` | 422 | An abort without a reason is not an abort |
| `UNKNOWN_ABORT_REASON` | 422 | The reason is not one the platform knows |
| `NOT_DRAFT` | 409 | The plan is frozen and can no longer be edited |
| `NOT_PROVISIONED` | 503 | No Site Profile is configured yet |
| `PROJECTION_UNAVAILABLE` | 503 | The projection store cannot be reached |

## Reading the mission resource

`status` and checkpoint `state` answer different questions and must not be
collapsed:

- `status` is the mission lifecycle: `DRAFT`, `READY`, `ACTIVE`, `COMPLETED`,
  `ABORTED`.
- checkpoint `state` is the coverage judgement: `PLANNED` (not judged yet),
  `VISITED` (a trusted observation passed it inside the window), `MISSED` (the
  window was observed reliably and nothing passed), `UNKNOWN` (the deciding
  evidence is not trustworthy).

A running mission therefore has no `MISSED` checkpoints — the unit may still
get there. `coverage.visited + coverage.missed` is below `coverage.total`
whenever checkpoints remain `PLANNED` or `UNKNOWN`, and clients must show that
gap rather than normalise it away.
