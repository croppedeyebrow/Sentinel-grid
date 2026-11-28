# ADR 0004: Telemetry ingest, per-sensor freshness and what the platform admits about itself

## Status

Accepted — Day 05–06, Telemetry Reliability slice, phase 1 (ingest and
projection). Phase 2, the live operator view, is not covered here.

## Context

Until this slice, observations were seeded SQL. The platform could reason about
freshness but never had to earn its data: nothing arrived, nothing was refused,
and no reading could be wrong about itself. This slice puts a real boundary in
front of the system — frames arrive from outside, some of them broken — and
the question becomes which of them may enter and what the platform is allowed
to claim once they have.

The planning documents fix part of it. `04_Rust_텔레메트리_통신.md` §3 makes
the gateway judge communication and leaves mission policy alone.
`07_데이터베이스_이벤트_아키텍처.md` requires the fact of receipt to survive
even when the value is not applied. `10_도메인모델_요구사항.md` TEL-01 asks for
per-sensor freshness, which ADR 0003 recorded as missing because one snapshot
row carried a single observation time for every field. The decisions below
either implement those or settle what they left open.

## Decisions

### 1. Transport is HTTP POST with a shared bearer token

The planning document aims at mTLS over framed TCP. Certificate issuance,
rotation and revocation are an operations concern, and building them now would
have bought identity we cannot yet administer. The token is required: the
gateway refuses to start without one rather than accepting unauthenticated
frames because a variable was forgotten. mTLS waits for Day 13, and the
gateway already owns everything that slice would not change — identity,
schema, size, ranges and sequence.

### 2. Only the gateway writes telemetry, and it judges communication only

Nothing else accepts frames, and the simulator holds no database credentials.
The gateway may refuse a frame claiming `battery=150`, because no battery is
ever 150; it may not refuse a battery of 4, because what a low battery means is
the mission policy's business. This keeps one boundary responsible for
admission and leaves judgement where the policy lives.

### 3. The gateway refuses what it cannot publish

A frame answered with a receipt but never forwarded is worse than a refused
one: nobody would go looking for it. When the stream is unreachable the gateway
returns `STREAM_UNAVAILABLE` and `/health/ready` reports 503. Senders retry;
the platform does not hold what it cannot pass on.

### 4. Everything that can be attributed is recorded, applied or not

`telemetry_receipts` holds every frame that reached the platform with the
outcome that befell it — accepted, duplicate, late, gapped or refused, with the
reason. A refused frame may not have parsed, so its unit, session and sequence
may be null; an applied one never can, which the table's constraints enforce.

The one thing deliberately **not** recorded is a frame that failed
authentication. It is not telemetry: the platform cannot attribute it to any
unit, and writing it would let an unauthenticated caller fill the log.

### 5. Freshness is judged per sensor, from two times per sensor

Each sensor carries its own measurement time and receipt time. A unit can hold
a position measured twenty minutes ago beside a battery measured six seconds
ago, and the console can say which one went stale instead of condemning the
row. The migration copied the old single pair into all three sensors before
dropping it, so nothing already stored lost its meaning.

Two times are kept because freshness means different things in two places: the
live picture asks "is this current" against now, while a mission's coverage
asks "was it trustworthy when taken" from the receipt delay — the rule ADR 0003
§3 set.

### 6. A sequence means nothing outside its session

A session is one connected run of one unit. Within it, sequence numbers
increase by one, and the gateway classifies every frame as `ACCEPTED`,
`DUPLICATE`, `OUT_OF_ORDER`, `GAP` or `REJECTED`. A reconnect opens a new
session and numbering restarts, which is why the classification is always made
inside a session rather than against the unit's history.

### 7. A session change is judged by measurement time, not arrival time

Measurement and receipt run on different clocks: a delayed frame is measured
early and received late. Judging a session by arrival let the network decide
which session a unit was on — a delayed frame from a session that had already
ended wound the stream's progress back to it, and a genuinely new session whose
first frames were older than what was stored was dismissed as that same late
traffic. Both sides of the comparison are now measurement times, and
`sessionStartedAt` is published as a measurement time for that reason.

### 8. The projection never moves a sensor backwards, and applying twice changes nothing

A sensor is replaced only by a strictly newer measurement, so a duplicate, a
reordered frame or a replayed receipt cannot rewrite history. Stream counters
advance only when the session's sequence does. This is what makes the consumer
safe to restart mid-batch, which it has to be, because entries are acknowledged
only after they are applied.

A reconnect resets the session's counters but keeps the readings. The unit was
there and measured them, and discarding the last known position at the moment
an operator most needs it would be the wrong kind of tidiness.

### 9. A gap is carried, not smoothed

The gateway reports how many frames were skipped, and the projection adds them
up for the current session. An incomplete stream stays visible as incomplete;
nothing is interpolated across the hole. The operator's picture may be
incomplete, but it is never quietly completed for them.

### 10. Redis Streams is transport, not a record

Redis runs without persistence here. Everything that must survive — the
projection and the receipt log — is in PostgreSQL, so a lost stream costs
delivery, not history. The consumer reads through a group, acknowledges after
applying, and recreates the group if it finds it gone rather than failing.

### 11. The platform reports its own intake as a separate fact

When ingest breaks, **every** unit falls silent at once. Per-unit silence alone
would therefore show a platform fault as a lost fleet. The Operational Picture
publishes `ingest.state` — `RECEIVING`, `NOT_RECEIVING` or `UNKNOWN` — beside
the units, and the console reads it before it reads the unit list. Unit-level
facts are left exactly as they are; the platform adds its own rather than
rewriting theirs. An instance that does not run the ingest path reports
`UNKNOWN`: it will not vouch for a path it has never touched.

### 12. Stream operations are bounded in time, and recovery is automatic

Every call to the stream has a two second ceiling, and the connection
re-establishes itself. A health endpoint that hangs during reconnection is
worse than one that answers 503, and a gateway that stayed refusing after the
stream returned would turn a brief outage into one lasting until somebody
noticed.

### 13. The simulator is a measuring instrument

Scenarios are declarative, seeded and reproducible, because a fault that cannot
be reproduced cannot be diagnosed. The faults injected are recorded apart from
the frames: comparing the platform's verdicts with the frames alone would be
circular. Each run's session ids carry its start, so re-running a manifest is a
new session rather than a replay the platform would rightly call duplicate.

## Consequences

- Telemetry has one way in. Every later producer — real units, replay, other
  sites — goes through the same admission rules, and nothing is tempted to
  write a snapshot directly.
- The receipt log is the audit record, which is why it stores the published
  sensors byte for byte. Re-encoding them once added a `null` position to
  frames that reported none, and the log claimed a reading that never arrived.
- Unit condition stays `UNKNOWN` more often than before, because per-sensor
  freshness exposes gaps the single timestamp used to hide. That is the honest
  answer, and ADR 0002's rule is unchanged.
- Verification now needs the real stack. The slice's completion gate — the
  projection's verdicts matching what the receipt log implies — cannot be
  measured against test doubles, and two of the four defects found in step 7
  were invisible to unit tests by construction.
- The console gained a platform-level reading. It can distinguish "this unit is
  quiet" from "we are not receiving", which no amount of per-unit data could
  have told it.

## Limitations

- **No back-pressure.** Publication is synchronous, so a burst is absorbed as
  latency and nothing is shed; 200 frames in a row were all accepted. The
  `OVERLOADED` code was removed from the contract rather than left as a promise
  nothing could emit. Shedding needs a bounded queue and a rule about what may
  be dropped first, which belongs with command dispatch (Day 07–08).
- **The resume point lives in gateway memory.** A restart loses it, and a
  reconnecting unit is then told it has no session; it starts a new one, which
  the projection handles as a reconnect. Durable resume points are not needed
  until store-and-forward devices are real.
- **`rejectedFrames` is cumulative while `missingFrames` is session-scoped.** A
  rejection is a fact about a unit's behaviour rather than about one session's
  sequence, but an unbounded counter tells an operator little. A window, or a
  rate, is the shape to fix it.
- **The live view is phase 2.** The console polls every ten seconds. Per-sensor
  freshness, reconnects and gaps are visible, but not pushed, and a change can
  sit unseen for a poll interval.
- **The gateway requires the stream at startup.** It recovers from an outage
  but will not boot through one. Acceptable while Compose orders the
  dependencies; a gateway that booted into refusal would be defensible too.
- **One consumer instance.** The group is there so a second can be added, but
  only one has ever run; partition behaviour is untested.
- **The duplicate window holds 1024 sequences per session.** A retransmission
  older than that reads as out of order rather than duplicate. Both are
  recorded and neither is applied over newer data, so the cost is a label.
- **Clock skew tolerance is five seconds and the frame limit 4 KiB**, both
  configurable but not yet derived from anything measured on real devices.
- **The seeded fixture and streamed data share the same tables.** Units nobody
  simulates stay silent and drift to `UNKNOWN`, which is correct but means a
  demo needs either the fixture tool or a simulator run.

## What to watch

Metrics remain Day 12 work; logs and the receipt log already carry what the
questions need. The series worth deriving:

- **Receipt outcomes per unit.** A unit whose frames are mostly refused is not
  a healthy unit, and today only a cumulative counter says so.
- **Gap receipts against `missingFrames`.** They must agree within a session.
  Divergence is how the projection's first real bug was found, and the same
  comparison is the completion gate.
- **`ingest.state` transitions and time to recover.** The drill measures five
  seconds; a rise in that number is a stream problem, not a fleet problem.
- **Publish timeouts and `STREAM_UNAVAILABLE` rates**, which say whether the
  two second ceiling is the right one.
- **Consumer lag** — stream length and pending entries for the group — which is
  the only early warning that projection has fallen behind ingest.
- **Per-sensor age distribution.** The share of units with one stale sensor is
  a measure of telemetry quality; the share with all three stale is a measure
  of coverage.

## Next improvements

1. Phase 2: WebSocket snapshot plus delta, so a reconnect or a gap reaches the
   operator when it happens rather than at the next poll.
2. Day 07–08 brings the bounded queue and the priority rule between commands
   and telemetry, which is what makes shedding — and `OVERLOADED` — real.
3. Day 11 replay reads the receipt log rather than the projection, which is why
   that log stores what arrived rather than what was applied.
4. Day 13 adds mTLS and, with it, device identity worth pinning a durable
   resume point to.
