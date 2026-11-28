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

### Telemetry Gateway (Day 05–06)

A separate service — the Rust communication boundary. It is the only writer of
telemetry: nothing else accepts frames, and the simulator holds no database
credentials.

| Method | Path | Request | Success | Notes |
| --- | --- | --- | --- | --- |
| POST | `/api/v1/telemetry/frames` | [frame](schemas/telemetry-frame.schema.json) | 202 [receipt](schemas/telemetry-receipt.schema.json) | `Authorization: Bearer <token>` required |
| GET | `/api/v1/telemetry/units/{unitId}/resume` | — | 200 [resume point](schemas/telemetry-resume.schema.json) | Reconnect handshake |
| GET | `/health/live` | — | 200 | Does not touch the stream |
| GET | `/health/ready` | — | 200 / 503 | 503 while the stream cannot be published to |

A frame may report any subset of the sensors, and it carries the moment the
values were **measured**, which is not the moment they arrive. Both times
travel with the event, because freshness means different things in the live
picture and in a mission's coverage — see
[ADR 0003](../docs/adr/0003-patrol-mission-lifecycle-and-coverage.md) §3.

The receipt tells the sender where its frame sat in the session's sequence
(`ACCEPTED`, `DUPLICATE`, `OUT_OF_ORDER`, `GAP`) and the resume point for a
reconnect. Refusals use [telemetry-error.schema.json](schemas/telemetry-error.schema.json),
whose vocabulary is the gateway's own: these are communication faults, not
mission faults.

### Telemetry stream

The gateway publishes [telemetry-event.schema.json](schemas/telemetry-event.schema.json)
to Redis Streams, and the Go consumer both applies it and stores it as the
receipt log. **Everything that arrived appears there, including what was
refused**, because `종합_기획_문서/07_데이터베이스_이벤트_아키텍처.md` requires
the fact of receipt to survive even when the value is not applied. A refused
frame may not have parsed, so its unit, session and sequence can be null; an
applied one never can.

The Operational Picture response carries what the projection makes of all this.
Each observation holds its own `observedAt` and `receivedAt`, so one sensor can
be stale beside a current one, and each unit carries a `stream` object:
`state` (`STREAMING`, `SILENT`, `NEVER_SEEN`), `sessionId`, `sessionStartedAt`,
`lastReceivedAt`, `reconnected`, `missingFrames` and `rejectedFrames`.

The response also carries `ingest`, the platform's account of its own intake:
`state` (`RECEIVING`, `NOT_RECEIVING`, `UNKNOWN`) and `lastEventAt`. It is
published separately from the units because a broken intake makes **every**
unit go silent at once, and a client that only had per-unit silence would
report a lost fleet instead of a lost pipe. `UNKNOWN` is what an instance that
does not run the intake path reports — it never vouches for a path it has not
touched. Per-unit facts are left as they are; the platform adds its own fact
rather than rewriting theirs.

`sessionStartedAt` is a **measurement** time — when the current session's first
frame was measured, not when it arrived. A session change is judged by
measurement order, because a delayed frame is measured early and received late:
comparing the two clocks would let the network decide which session a unit is
on, and a frame of a session that already ended would wind the stream back.

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

## Telemetry gateway codes

The gateway refuses frames, not requests, so it has its own vocabulary. Every
refusal is still recorded as received, with the code and the facts.

| Code | HTTP | Meaning |
| --- | --- | --- |
| `UNAUTHENTICATED` | 401 | Missing or wrong token |
| `MALFORMED_FRAME` | 400 | Not a frame: bad JSON, missing or undeclared field, empty identifier |
| `FRAME_TOO_LARGE` | 413 | Larger than the gateway accepts; refused before parsing |
| `NO_SENSOR_VALUE` | 422 | The frame reports no sensor, so there is nothing to apply |
| `VALUE_OUT_OF_RANGE` | 422 | A reported value cannot be true (`battery=150`, latitude 95) |
| `INVALID_SEQUENCE` | 422 | Sequence numbers start at 1 |
| `MEASURED_IN_FUTURE` | 422 | Measured further ahead of the gateway clock than the allowed skew |
| `UNKNOWN_UNIT` | 404 | The frame names a unit the platform does not know |
| `STREAM_UNAVAILABLE` | 503 | The gateway cannot publish, so it refuses rather than accepting what it cannot forward |

Publication is synchronous: a frame is answered once it is on the stream, so a
burst is absorbed as latency, not as shedding. There is no `OVERLOADED` code
because nothing can emit one yet — load shedding needs a bounded queue and a
priority rule between telemetry and commands, which arrives with command
dispatch (Day 07–08). What is bounded today is the stream itself, which is
trimmed to a maximum length as it is written.

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
