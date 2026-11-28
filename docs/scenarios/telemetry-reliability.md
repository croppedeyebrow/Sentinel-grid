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

---

# Phase 2 — live operator view

Scope definition for the second half of the slice, written before any socket
code exists. Steps 1 and 2 of the slice order, continued from the phase 1
sections above.

Phase 1 made the platform's knowledge correct. Phase 2 makes it arrive. Today
the console polls every ten seconds, so a unit can drop and come back inside
one interval and the operator never learns it happened — the projection holds
the truth, the screen holds a version of it up to ten seconds old, and nothing
on screen says which.

## Users

| User | Does |
|---|---|
| Patrol operator | Watches the picture change as it changes: a sensor going stale, a unit falling silent, a unit coming back |
| Patrol operator | Needs to know when the screen itself is behind, and not mistake a frozen screen for a quiet fleet |
| Reliability engineer | Confirms the pushed picture never disagrees with what a plain read of the API would return |

## Starting conditions

- The ingest path works end to end: the gateway admits frames, the consumer
  applies them, and the projection and receipt log agree (phase 1 completion
  gate).
- `GET /api/v1/operational-picture` serves the whole picture, including
  per-sensor freshness, each unit's `stream` and the platform's `ingest`.
- The console reads that endpoint on a ten second interval and has no notion of
  being live or behind.
- The consumer already knows exactly what each applied event changed — which
  sensors, whether it was a reconnect — and throws that knowledge away.

## Flow

1. **The console opens a socket** and is sent the whole picture first, so it
   never starts from a partial view.
2. **The consumer applies an event** and hands the change to the hub. The hub
   sends the units that actually changed, nothing else.
3. **The console applies the change** and marks what moved, so the operator's
   eye is drawn to the unit that changed rather than to the whole list.
4. **The platform's own intake changes** — ingest breaks or recovers — and that
   is pushed too, because it is the fact that decides how every unit's silence
   should be read.
5. **A message is missed.** The console notices the gap in the stream's
   revisions and asks for a fresh picture rather than carrying on with a
   version it knows is incomplete.
6. **The socket drops.** The console falls back to polling and says so on
   screen. When the socket returns it starts again from a whole picture.

## Decisions taken up front

- **The push is driven by applying, not by a timer.** The consumer is where the
  change is known, so that is where it is announced. A timer would reintroduce
  the delay this phase exists to remove, and would read the projection on a
  schedule to discover what the consumer already knew.
- **A delta carries whole units, not field patches.** The client replaces a
  unit by id. Applying the same delta twice leaves the same screen, and no
  merge rule has to live in the browser. Patches would buy bandwidth this
  scale does not need and cost the client a reconciliation it can get wrong.
- **Every message carries a revision, and a gap forces a resync.** The client
  that notices it missed something asks for the whole picture instead of
  guessing. A screen that silently skips a change is worse than one that
  reloads.
- **A client that cannot keep up is disconnected, not quietly trimmed.** Each
  connection has a bounded buffer; a connection that overflows it is closed and
  the client reconnects into a fresh snapshot. Dropping messages to keep a slow
  client attached would leave it showing a picture nobody can describe.
- **Falling back to polling is visible.** The screen says when it is not live.
  This is the same rule as phase 1's `ingest` axis: the platform states what it
  does not know rather than letting a stale screen look current.
- **The socket is unauthenticated for now, like the REST endpoints.** It is
  served from the same service, restricted by the same origin rules.
  Authority is Day 12 (SEC-01), and adding a second, different answer here
  would have to be redone then.
- **The socket is additive.** `GET /api/v1/operational-picture` keeps working
  unchanged, which is what makes the fallback possible and keeps the contract
  test for the REST shape meaningful.

## Message rules

One socket, one direction for data. The client sends only a resync request.

| Message | When | Carries |
|---|---|---|
| `snapshot` | On connect, and on any resync | The whole picture: site, areas, routes, every unit, `ingest`, and the current revision |
| `units` | A unit's projection changed | Only the changed units, whole, with the new revision |
| `ingest` | The platform's intake changed state | The `ingest` object and the new revision |
| `resync` (client → server) | The client saw a revision gap | Nothing; the server answers with a `snapshot` |

Rules the messages obey:

- The revision increases by one per message on a connection. It is a
  per-connection counter, not a global one: it answers "did I miss something",
  not "which version of the world is this".
- A `snapshot` resets the client's expectation to its own revision, so a
  resync cannot leave the client permanently suspicious.
- Nothing is pushed that did not change. An event that applied nothing — a
  duplicate, a stale sensor — produces no message.
- A pushed unit is the same shape as a unit in the REST response, validated
  against the same schema. There is no second unit shape to keep in step.

## Screen draft (step 2)

The phase 1 draft deferred this. The unit card and the stream row already
exist, so what this draft decides is what *liveness* looks like and what the
operator must be able to tell apart.

| Region | Shows | Why |
|---|---|---|
| Topbar liveness | `실시간` / `폴링` / `연결 중`, with the age of the last message | A frozen screen and a quiet fleet look identical without it |
| Unit card | A brief highlight on the unit that just changed, and which sensor it was | Change on a six-unit list is otherwise invisible; the operator should not have to diff the screen themselves |
| Unit card | The reconnect badge the phase 1 stream row already carries, now appearing the moment it happens | A reconnect is the event an operator most wants to catch, and it is the one most likely to fall inside a poll interval |
| Ingest banner | Unchanged from phase 1, but now updated by push | The platform's silence should reach the screen faster than the units' silence |
| Resync notice | A brief line when the client reloaded because it missed messages | An operator who sees the list flash should know why |

What must not change:

- The two axes. A unit that goes silent is still `UNKNOWN`, never a failure,
  and a highlight marks *a change*, not *a problem*.
- No interpolation. A delta for a unit replaces that unit; a unit nobody
  mentioned keeps its previous reading and keeps ageing against the policy.
- Ages still count against the moment the server judged the picture, which
  means a snapshot's `generatedAt` and a delta's own timestamp both travel.

Screen state stays screen state: which unit is highlighted, whether the list
is scrolled, the resync notice. Server state stays server state: the picture
itself. The live connection does not become a place to keep UI decisions.

## Success

- A unit that drops and comes back is visible on screen without a reload, and
  within a second of the frame arriving.
- A sensor going stale appears as that sensor going stale, not as the unit
  changing wholesale.
- The pushed picture never disagrees with a plain read: after any sequence of
  deltas, a fresh `GET` returns the same verdicts the screen is showing — the
  phase's completion gate.
- Killing the socket leaves a working screen that says it is not live, and
  restoring it returns to live without a reload.
- A client that misses messages reloads instead of showing a picture built on
  a gap.
- Nothing is pushed for an event that changed nothing.

## Failure outcomes to exercise

| Fault injected | Expected |
|---|---|
| Socket refused at page load | Console polls from the start and says it is not live; no error state |
| Socket dropped mid-session | Falls back to polling within one retry, badge changes, no data lost on reconnect |
| Service restarted | Clients reconnect and receive a fresh snapshot; no client shows a pre-restart picture |
| Revision gap on the wire | Client requests a resync and the screen is whole again, with a notice |
| Slow client (socket not read) | Server closes that connection after its buffer fills; other clients unaffected |
| Ingest breaks while connected | `ingest` push arrives, the banner appears without waiting for a unit to change |
| Duplicate or stale event applied | No message is sent at all |
| Many clients connected | Each gets its own snapshot and the same deltas; one slow client does not delay the others |
| Redis unavailable | Deltas stop because nothing is being applied; the ingest push says why, and the screen stays live |

## Verification plan

| What | Where |
|---|---|
| Revision and gap handling | Go unit tests on the hub; console tests on the client reducer |
| Delta is idempotent per unit | Console test: applying the same delta twice leaves the same model |
| Nothing pushed for a no-op apply | Go test: an apply result that changed nothing produces no message |
| Slow client is dropped, not trimmed | Go test with a blocked consumer of the connection |
| Fallback and recovery | Console tests with a failing socket, then a working one |
| Push and read agree | End to end: run a scenario with the socket open, then compare the screen's model with a fresh read — the completion gate |
| Reconnect latency | Measured on the live stack: frame received to screen updated |
