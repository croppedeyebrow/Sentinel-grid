# Telemetry Reliability scenario

Scope definition for the Day 05–06 slice, written before any gateway or
simulator code exists. Step 1 of the common slice order in
`docs/00_도메인_수직슬라이스_개발_가이드.md`.

This slice is where telemetry stops being a fixture. Until now observations
were seeded SQL; from here a simulator produces them, a gateway decides which
ones may enter the platform, and the projection has to end up at the same
freshness verdict as the raw event log.

## Users

| User | Does |
|---|---|
| Patrol operator | Reads which sensor is current and which is not, per unit, and sees a unit drop and come back |
| Reliability engineer | Runs a named fault scenario and compares the projection against the events that were actually received |

## Starting conditions

- The Operational Picture and Patrol Mission slices are in place: six seeded
  units, one Site Profile with its freshness policy, and missions whose
  coverage is judged from observations.
- Observations arrive only from the seed and the fixture tool. Nothing streams.
- One snapshot row carries a single `observed_at` for position, battery and
  link together — the limitation recorded in
  `docs/adr/0003-patrol-mission-lifecycle-and-coverage.md`.

## Flow

1. **A scenario is declared.** A manifest names the units, the duration, the
   tick rate, a fixed seed, and the faults to inject. The same manifest run
   twice produces the same frames.
2. **The simulator sends frames.** Each frame carries the unit, a session id, a
   sequence number, the moment the value was measured, and any subset of the
   sensors — position, battery, link. A frame may carry only one of them.
3. **The gateway decides.** It authenticates the sender, checks the frame
   against the published schema, checks size and value ranges, and classifies
   the sequence: accepted, duplicate, out of order, or a gap. Validated frames
   are normalised and published to the telemetry stream.
4. **Nothing received is thrown away.** A rejected, duplicated or late frame is
   recorded with the reason it was not applied, because
   `종합_기획_문서/07_데이터베이스_이벤트_아키텍처.md` requires the fact of
   receipt to survive even when the value is not applied.
5. **The projection applies per sensor.** The consumer updates each sensor
   independently and never lets an older measurement overwrite a newer one, so
   a unit can hold a stale position next to a fresh battery.
6. **The console shows it per sensor.** Each sensor carries its own freshness,
   and a unit that disconnects and returns is shown as such rather than
   silently frozen. *(Phase 2.)*

## Scope: two phases

**Phase 1 — ingest and projection**

- Per-sensor observation and snapshot schema (TEL-01).
- Python simulator driven by a scenario manifest, with a fixed seed and fault
  injection; `--loop` for a live demo.
- Rust gateway: token authentication, schema and size validation, range checks,
  session and sequence classification, normalisation, publication.
- Redis Streams between the gateway and Go, with a consumer group.
- Go consumer and projection, idempotent per session and sequence.
- Receipt log of everything that arrived, applied or not.

**Phase 2 — live operator view**

- WebSocket snapshot plus delta from Go, sequence gap handling on the client.
- Per-sensor freshness, partial updates, disconnect and reconnect on screen.

## Decisions taken up front

- **Transport is HTTP POST with a shared token.** The planning document aims at
  mTLS and framed TCP; certificate issuance, rotation and revocation are an
  operations concern and wait for Day 13. The gateway still owns identity,
  schema, size, range and sequence, which is what this slice is about.
- **The simulator is manifest-driven.** A fault test that cannot be reproduced
  cannot be diagnosed, so scenarios are declarative and seeded.
- **Only the gateway may write telemetry.** The simulator has no database
  credentials, and nothing else accepts frames.
- **The gateway judges communication, not missions.** It may reject a frame
  claiming `battery=150`, but a low battery is the mission policy's business,
  as `종합_기획_문서/04_Rust_텔레메트리_통신.md` §3 requires.

## Sequence and session rules

A session is one connected run of one unit. Within a session, sequence numbers
increase by one.

| Classification | Condition | What happens |
|---|---|---|
| `ACCEPTED` | `sequence == last + 1`, or the first frame of a session | Normalised, published, applied |
| `DUPLICATE` | `sequence` already seen in this session | Recorded, not applied |
| `OUT_OF_ORDER` | `sequence < last` and not seen | Recorded, applied **only** where a sensor's measurement is newer than the stored one |
| `GAP` | `sequence > last + 1` | Accepted and applied; the missing range is recorded so the operator knows the stream is incomplete |
| `REJECTED` | Bad token, unknown unit, schema violation, oversized frame, value out of range, or a measurement time in the future | Recorded with the reason, never published |

A reconnect starts a new session. The gateway answers the handshake with the
last sequence it accepted for that unit, so a simulator holding
store-and-forward frames knows where to resume.

## Per-sensor freshness

Each sensor carries its own measurement and receipt time. The freshness rules
from `docs/adr/0002-two-axis-unit-status.md` then apply per sensor instead of
per row, which is TEL-01.

The unit condition axis is unchanged: if any required sensor is not `FRESH`,
the condition stays `UNKNOWN`. What changes is that the console can now say
*which* sensor went stale, and a stale position no longer makes a fresh battery
look stale.

## Success

- A named scenario run twice produces the same frames and the same verdicts.
- A unit shows a stale position next to a fresh battery, from real streamed
  data.
- Duplicate and out-of-order frames never move a sensor backwards in time.
- A gap is visible as a gap, not as a silent continuation.
- Everything received appears in the receipt log with its outcome, including
  what was rejected.
- The projection's freshness verdict matches what the raw event log implies —
  the slice's completion gate.

## Failure outcomes to exercise

| Fault injected | Expected |
|---|---|
| Delayed frame (received long after it was measured) | Applied to the projection, but not trusted as a current reading, and not usable as coverage evidence |
| Duplicated frame | Recorded as `DUPLICATE`, projection unchanged |
| Reversed order | Older measurement never overwrites a newer one |
| Sequence gap | Applied, gap recorded, operator can see the stream was incomplete |
| Link loss then reconnect | New session, handshake reports the last accepted sequence, buffered frames resume without duplicates |
| Oversized or malformed frame | `REJECTED` before publication, recorded with the reason |
| Value out of range (`battery=150`, latitude 95) | `REJECTED`, and the previous good value stays |
| Measurement timestamped in the future | `REJECTED` rather than accepted as the newest value |
| Bad or missing token | `401`, nothing recorded as telemetry |
| Redis unavailable | The gateway refuses new frames rather than accepting what it cannot publish; `/health/ready` reports it |
| Burst of frames | Bounded queue, oldest ordinary telemetry dropped or merged first, and the drop is counted |

## Out of scope for this slice

- mTLS, certificate issuance and revocation (Day 13).
- Command dispatch, ACK and priority between command and telemetry queues
  (Day 07–08).
- Anomaly detection and analysis service (Day 09–10).
- Replay of historical state (Day 11).
- Multi-site routing and authority checks (Day 12).

## Screen draft (step 2, abbreviated)

Phase 1 produces no new screen, so the UI draft was kept to deciding what the
console will have to show once the stream is live. The full draft is made at
the start of phase 2.

What is already there: the unit card shows position, battery and link, each
with its own freshness chip and age. Per-sensor freshness therefore needs no
new layout — it needs the data to actually differ per sensor, which is what
phase 1 delivers.

What is missing, and has to come from the API:

| Reading | Why the operator needs it |
|---|---|
| Session state per unit — streaming, reconnected, or silent | A unit that stopped sending looks identical to a unit sitting still unless the session says otherwise |
| Last received moment, separate from last measured | Distinguishes "nothing is being measured" from "nothing is arriving" |
| Missing sequence count in the current session | A gap means the picture is incomplete; hiding it would overstate what is known |
| Rejected frame count per unit | A unit whose frames are all rejected is not a healthy unit, and the operator cannot see that today |

Those four become fields on the Operational Picture response in step 4, and the
unit card grows one "stream" row for them. The two axes stay as they are: these
readings are data quality, not unit condition, and a silent unit remains
`UNKNOWN` rather than becoming a failure.

## Verification plan

| What | Where |
|---|---|
| Sequence and session classification | Rust unit tests, table driven over the five classifications |
| Schema, size and range validation | Rust unit tests, including a malformed and an oversized frame |
| Per-sensor application rules | Go projection tests: an older measurement must not overwrite a newer one |
| Idempotency | Go consumer tests: the same session and sequence applied twice changes nothing |
| Scenario determinism | Python test: the same manifest and seed produce identical frames |
| Fault injection | Python tests per fault: delay, duplicate, reorder, gap, disconnect |
| End to end | A scenario run against the real stack, comparing the projection's verdicts with the receipt log |
| Live view | Console tests for per-sensor freshness, partial update, disconnect and reconnect *(phase 2)* |

## Verification results — phase 1

Step 7 of the slice order. Measured against the running stack: PostgreSQL,
Redis and the gateway in containers, the Mission API and its consumer on the
host, frames from the simulator. Where a row says *measured*, the number comes
from the live stack and not from a test double.

```powershell
docker compose up -d --wait
cd services\mission-api; go run ./cmd/api
cd python\patrol-simulator; uv run patrol-simulator scenarios\unreliable-link.yaml --token local-telemetry-token
```

### Failure outcomes

| Fault injected | Result | Evidence |
|---|---|---|
| Delayed frame | Applied where its measurement is still the newest for that sensor, never trusted as current | Go domain test (a delayed frame does not replace a newer one, `lastReceivedAt` still moves); measured: `OUT_OF_ORDER` ×2 recorded and applied per sensor |
| Duplicated frame | `DUPLICATE`, projection unchanged | Rust classification tests; Go domain test; measured: re-sending an accepted sequence answers `DUPLICATE` and the snapshot does not move |
| Reversed order | Older measurement never overwrites a newer one | Go domain tests; measured in the `unreliable-link` run |
| Sequence gap | Applied, gap counted, visible per unit | Go domain + integration tests; measured: 3 gap receipts ↔ `missingFrames = 3` |
| Link loss then reconnect | New session, counters reset, readings kept; handshake reports the last accepted sequence and the resumed frame is accepted while a re-sent one is a duplicate | Go domain tests; measured: `GET …/resume` → `lastAcceptedSequence: 12`, sequence 13 → `ACCEPTED`, sequence 12 again → `DUPLICATE` |
| Oversized or malformed frame | `FRAME_TOO_LARGE` (413) and `MALFORMED_FRAME` (400), refused before publication, recorded | Rust unit tests; measured, including an undeclared field |
| Value out of range | `VALUE_OUT_OF_RANGE` (422), previous good value kept | Measured: `battery=150` and `latitude=95` refused, stored battery unchanged at 63 |
| Measurement in the future | `MEASURED_IN_FUTURE` (422) | Measured: 299 s ahead refused, previous value kept |
| Bad or missing token | `401`, and **nothing recorded as telemetry** | Measured: 10 bad frames produced 8 receipts — the two unauthenticated ones left no record, because the gateway refuses before it can attribute them |
| Redis unavailable | Gateway refuses with `STREAM_UNAVAILABLE` and reports 503; the API keeps serving reads and the picture reports `ingest.state = NOT_RECEIVING`; both recover without a restart | Measured in the dependency drill (README); recovery 5 s |
| Burst of frames | **Not implemented.** Publication is synchronous, so a burst is absorbed as latency, not shed; 200 frames were all accepted. The stream is trimmed to a maximum length, which bounds memory but is not back-pressure | Measured; see limitations below |

### Success criteria

| Criterion | Result |
|---|---|
| A named scenario run twice produces the same frames and verdicts | Python determinism test; measured: two `unreliable-link` runs returned identical outcome counts |
| A stale position beside a fresh battery, from real streamed data | Measured: `position STALE` observed 09:16:20, `battery FRESH` observed 09:31:50 — a 930 s gap in one row, unit condition `UNKNOWN` |
| Duplicate and out-of-order frames never move a sensor backwards | Go domain tests + measured |
| A gap is visible as a gap | Measured: `missingFrames` matches the gap receipts |
| Everything received appears in the receipt log with its outcome | Measured: every refusal except the unauthenticated ones, which never become telemetry |
| The projection's freshness verdict matches the raw event log — *the completion gate* | **Measured:** for every unit and sensor, the stored measurement time equals the newest applied receipt carrying that sensor, with a 930 s per-sensor lag present |

### What verification found

Four defects, none of which the unit tests could have seen:

1. **The reconnect handshake answered 404 to everyone.** The route declared its
   parameter in a syntax the router does not use, so it matched no URL while
   the handler next to it kept working. Now guarded by a routing test that
   requests a concrete unit URL, which fails on the old spelling.
2. **The receipt log claimed readings that never arrived.** The consumer
   re-encoded the parsed sensors, which wrote `"position": null` for frames
   that reported no position. The audit record now stores the published
   sensors byte for byte.
3. **Per-sensor freshness was never demonstrated from streamed data.** Every
   simulated frame carried all three sensors, so TEL-01 could only be shown
   from the seeded fixture. The simulator gained a `sensor_dropout` fault and
   the `partial-sensors` scenario, which is what the measurement above uses.
4. **The contract published a refusal nothing could emit.** `OVERLOADED` was
   removed: load shedding needs a bounded queue and a priority rule between
   telemetry and commands, which belongs with command dispatch.

The gateway's two recovery defects — a connection that never re-established
and a health endpoint that hung during reconnection — were found by the same
drill in step 6 and are recorded in the README.

### Limitations carried out of phase 1

- **No back-pressure.** A burst is absorbed as latency. Shedding, and the
  decision about what may be dropped first, waits for command dispatch
  (Day 07–08).
- **The resume point lives in gateway memory.** A gateway restart loses it, and
  a reconnecting unit is then told it has no session — it starts a new one,
  which the projection handles as a reconnect. Durable resume points are not
  needed until store-and-forward devices are real.
- **`rejectedFrames` is cumulative, not session-scoped**, while
  `missingFrames` resets with the session. A rejection is a fact about the
  unit's behaviour rather than about one session's sequence, but an unbounded
  counter is of limited use to an operator.
- **The live operator view is phase 2.** The console polls; per-sensor
  freshness, partial updates and reconnects are visible but not pushed.
