# ADR 0003: Patrol Mission lifecycle, coverage judgement and write safety

## Status

Accepted — Day 03–04, Patrol Mission slice.

## Context

The slice had to answer a different question from the Operational Picture: not
"what is the state of this unit" but "what did we instruct, and was it actually
patrolled". That brought three things into the system for the first time —
mutations, a lifecycle with legal and illegal transitions, and a judgement made
after the fact from stored observations.

The planning documents fix some of it already. `10_도메인모델_요구사항.md` sets
the transitions and invariant 6 (coverage counts only observations meeting the
quality conditions). `03_Go_임무API_아키텍처.md` requires optimistic locking and
idempotency on mutations, and names the invariants of PatrolMission and
Assignment. `도메인_지식/00_도시_지역순찰_운용.md` puts safe interruption and
honest reporting of unpatrolled segments above completing a route, and requires
the flight area, time window and restrictions to be fixed to the mission
version. Everything below either implements those or decides what they left
open.

## Decisions

### 1. Checkpoints and assignments belong to the mission

`03_Go_임무API_아키텍처.md` §5 lists `/checkpoints` as its own endpoint, but
approval has to freeze the plan. A checkpoint shared between missions could be
moved after approval and silently change what a finished mission's coverage
meant. They are therefore sub-resources: `mission_checkpoints` and
`mission_assignments` are owned by a mission, editable only while it is a
draft, and frozen from approval onwards (`NOT_DRAFT`).

### 2. Coverage is judged at read time, and has four values

The plan stores checkpoints; it stores no verdict. Each read derives the state
of every checkpoint from the observations and the pinned policy:

| Value | Meaning |
| --- | --- |
| `PLANNED` | The mission has not reached it. Not coverage, not a miss. |
| `VISITED` | A trusted observation fell inside the radius during the window. |
| `MISSED` | The window was observed reliably and nothing passed it. |
| `UNKNOWN` | The deciding evidence is not trustworthy. |

`PLANNED` is the value the UI draft (step 2) discovered was missing: without it
a running mission's un-reached checkpoints would have to be called missed or
visited, and both would be false. A running mission therefore never reports a
`MISSED` checkpoint.

### 3. Evidence is trusted by receipt delay, not by age

Freshness in the Operational Picture means "is this value current", measured
against now. Coverage asks something else, because the mission is being judged
after it ran: an observation from two hours ago is good evidence if it was
trustworthy when taken. An observation qualifies when it carries a position and
an observation time, and reached the platform within the Site Profile's
position window (`received_at - observed_at`). A packet that arrived long after
it was measured cannot prove where the unit was at a moment, and a reversed
pair (received before observed) is refused as a clock disagreement.

Unusable rows are kept with `Trusted = false` rather than dropped, so "we
received something we cannot use" stays distinguishable from "we received
nothing".

### 4. A miss requires evidence, and completion does not change that

`MISSED` is reported only when the window held at least one trusted
observation. Completed and aborted missions follow the same rule: a mission
aborted for battery with healthy telemetry reports its remaining checkpoints as
unpatrolled, because the city operating notes put showing unpatrolled segments
first; a mission aborted because the link died reports `UNKNOWN`, because
nothing can be concluded. The outcome of the mission does not decide the
coverage verdict — the evidence does.

### 5. An abort is a normal outcome and records its cause

`ABORTED` is not an error path. It carries a required reason (`OPERATOR`,
`BATTERY`, `LINK`, `SAFETY`, `POLICY`) and the moment, enforced by the
aggregate (`ABORT_REASON_REQUIRED`) and by a database constraint. Day 09–10
incident management then reads a recorded cause instead of inferring one.

### 6. Every transition carries the version the caller read

Optimistic locking lives in the domain, not in the adapter: `Approve`, `Start`,
`Complete` and `AbortWith` all take the caller's `expectedVersion` and refuse a
stale one (`STALE_VERSION`, with both versions in the refusal). The repository
makes the update conditional on that version, so two operators acting at once
cannot overwrite each other. Creation is guarded by a required
`Idempotency-Key`, checked by the service and enforced by a unique key in the
database, so a retried request cannot plan a second mission.

### 7. A draft is not a commitment

Only approved plans (`READY`, `ACTIVE`) hold a unit. Two drafts may name the
same unit for overlapping windows; the first approval takes it and the second
is refused with `ASSIGNMENT_CONFLICT` naming the callsigns. Planning stays
free; committing is exclusive.

### 8. The policy version is pinned at approval

The contract published `policyVersion`, but the Site Profile had no revision to
pin, so the field pointed at nothing. `site_profiles.policy_version` was added
(migration 004) and approval copies the current value onto the mission. A later
policy change cannot move a running mission's judgements.

### 9. Containment is decided in the domain, from PostGIS geometry

The boundary is read from PostGIS; the containment decision is a pure function
in the domain, so approval is decidable in a unit test and the refusal can name
the offending checkpoint. The console runs the same check while placing
checkpoints, as feedback only. Database constraints back the invariants the
database can express (no aborted mission without a reason, no running mission
without an approval, window order, unique checkpoint sequence), so a bug in the
service cannot persist a state the domain would reject.

### 10. Refusals are part of the contract

Every refusal returns a `code` the client keys off and `details` carrying the
facts needed to act: which units conflict, which checkpoint lies outside the
area, which version was expected. The console maps codes to its own wording and
never shows the server's English sentence. Request logs carry the same
`request_id` as the response header, so a refusal an operator reports can be
traced to the request that produced it.

## Consequences

- The mission resource is large: the plan, the judgement, the coverage counts
  and the allowed transitions come in one response. In exchange the console
  renders list and detail from a single request and never re-derives a verdict.
- `allowedTransitions` is served by the API. The console offers exactly those,
  which removed the transition table it kept during the mock phase.
- Reads are pure and repeatable: the same stored rows and the same policy
  produce the same verdicts, which is what makes the Day 11 replay slice
  possible without storing verdict history.
- Coverage percentages do not sum to 100 while checkpoints are `PLANNED` or
  `UNKNOWN`, and clients must show that gap rather than normalise it.

## Limitations

- **Assignment conflict has a race window.** The conflict query and the
  approval are not serialised, so two approvals issued at the same moment can
  both pass. An exclusion constraint on `mission_assignments` cannot express
  "only while the mission is not terminal" without denormalising the status;
  the fix belongs with the control lease work in Day 07–08.
- **The mission list runs one observation query per mission.** Acceptable at
  V01 scale, and the shape to fix (one windowed query joined across missions)
  is known.
- **One snapshot row carries one observation time for all fields.** Per-sensor
  timestamps arrive with Telemetry Reliability (Day 05–06, TEL-01); until then
  a field is only ever missing when its value is absent.
- **No route-order reasoning.** A checkpoint skipped out of order is not
  reported as missed while the mission runs, only after it closes.
- **A draft edit is not version-guarded.** Checkpoint and assignment edits are
  refused outside `DRAFT` but do not require the caller's version.
- **Single site.** The service serves the first Site Profile; multi-site
  routing is out of scope for V01.
- **Authority is not modelled.** Who may approve or start is the permission
  matrix of Day 12 (SEC-01); this slice models the transitions only.
- **No events.** Nothing is published on a transition; the outbox arrives with
  Day 07–08.

## What to watch

Logs carry what is needed to answer the operational questions; metrics are
Day 12 work. The useful series to add there, derived from what is already
logged:

- refusals per `code` (a rise in `STALE_VERSION` means operators are working on
  stale screens; a rise in `ASSIGNMENT_CONFLICT` means planning collides),
- the share of checkpoints judged `UNKNOWN` per mission, which is the honest
  measure of telemetry quality rather than of patrol performance,
- transition latency and the 5xx rate per endpoint.

## Next improvements

1. Day 05–06 per-sensor freshness sharpens coverage: a unit with a fresh
   position but stale battery can then credit a pass without claiming the
   battery was known.
2. Day 07–08 adds the control lease and the outbox, which also closes the
   assignment-conflict race and makes transitions publish events.
3. Day 12 adds the permission matrix and the metrics above.
