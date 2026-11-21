# Patrol Mission scenario

Scope definition for the Day 03–04 slice, written before any mission code
exists. It fixes who acts, what state the system starts in, what the operator
does, and which outcomes count as success or failure — step 1 of the common
slice order in `docs/00_도메인_수직슬라이스_개발_가이드.md`.

## Users

| User | Does |
|---|---|
| Mission planner | Defines the patrol area to use, places checkpoints on the map, sets the operating window, assigns units, and approves the plan |
| Patrol operator | Starts the approved mission, watches progress, aborts it when continuing is unsafe |

Separating *who may approve* from *who may start* is a permission matrix
question (SEC-01) and belongs to the Day 12 integration slice. This slice
models the transitions themselves, not the authority to perform them.

## Starting conditions

- One city Site Profile ("Sentinel Test Site") with its observation policy.
- One patrol area (`Primary Patrol Area`, a polygon) and one planned route
  (`Riverside Loop`, a line string) already seeded by the previous slice.
- Six patrol units with their latest observations, carrying the two axes from
  `docs/adr/0002-two-axis-unit-status.md`: condition and per-field freshness.
- No missions, no checkpoints, no assignments exist yet.

## Flow

1. **Draft.** The planner picks the patrol area and clicks points on the map to
   place checkpoints in order. Each checkpoint has a pass radius. The mission
   is created as `DRAFT`.
2. **Assign.** The planner assigns one or more units to the mission for an
   operating window.
3. **Approve.** The plan is frozen: area, route, checkpoints and the Site
   Profile policy version are pinned to the mission, which moves to `READY`.
   `도메인_지식/00_도시_지역순찰_운용.md` requires the flight area, time window
   and restrictions to be fixed to the mission version rather than read live.
4. **Start.** The operator starts the mission, which moves to `ACTIVE`.
5. **Progress.** As observations arrive, each checkpoint is judged passed or
   not, and coverage is recomputed from those judgements.
6. **Finish.** The mission ends as `COMPLETED` when the planner closes it out,
   or as `ABORTED` when the operator stops it early. An aborted mission is a
   normal outcome, not an error: the city operating notes put safe interruption
   and honest reporting of unpatrolled segments above finishing the route.
   An abort therefore records a required reason — `OPERATOR`, `BATTERY`,
   `LINK`, `SAFETY` or `POLICY` — with the moment it was taken, so the incident
   workflow in Day 09–10 reads a recorded cause instead of inferring one later.

## State transitions

`DRAFT → READY → ACTIVE → COMPLETED` or `ACTIVE → ABORTED`, as fixed in
`종합_기획_문서/10_도메인모델_요구사항.md`.

| From | Event | To | Rejected when |
|---|---|---|---|
| `DRAFT` | approve | `READY` | No checkpoints, no assigned unit, or a checkpoint outside the area |
| `DRAFT` | abort | `ABORTED` | No abort reason given |
| `READY` | start | `ACTIVE` | The mission was never approved, or its assigned unit is already active elsewhere in the window |
| `ACTIVE` | complete | `COMPLETED` | — |
| `ACTIVE` | abort | `ABORTED` | No abort reason given |
| `COMPLETED`, `ABORTED` | anything | — | Terminal states accept no transitions |

Every transition carries the mission version the caller last saw. A transition
against a stale version is rejected rather than applied, so two operators
acting at once cannot overwrite each other's outcome.

## Invariants

1. A mission cannot start without an approved area, route, checkpoints and
   policy version (`03_Go_임무API_아키텍처.md` §3).
2. One unit cannot hold two overlapping assignments (same document).
3. Every checkpoint must lie inside the mission's patrol area, verified
   server-side with PostGIS containment.
4. **Coverage counts only observations that satisfy the quality conditions**
   (invariant 6 in `10_도메인모델_요구사항.md`). This is the point where the
   previous slice feeds this one.
5. The same create request repeated must not produce two missions
   (`Idempotency-Key`), and a transition built on a stale mission version is
   refused (optimistic locking). Control lease and outbox publication stay with
   the Command slice in Day 07–08.
6. An abort without a reason is not a valid abort.

## Coverage judgement

A checkpoint is in exactly one of three states, mirroring the two-axis
decision: the platform does not turn absent evidence into either outcome.

| State | Condition |
|---|---|
| `VISITED` | A `FRESH` observation fell inside the checkpoint radius while the mission was active |
| `MISSED` | The mission reached a terminal state with trustworthy observations covering the window, and none of them passed this checkpoint |
| `UNKNOWN` | The observations that would decide it are `STALE` or `MISSING` |

- Coverage percentage counts `VISITED` against the total.
- Unpatrolled segments are the `MISSED` checkpoints and the route legs between
  them.
- `UNKNOWN` checkpoints are reported separately and are never folded into
  either coverage or unpatrolled. The city verification questions ask
  explicitly whether the UI hides unpatrolled segments; collapsing `UNKNOWN`
  into `VISITED` would overstate coverage, and into `MISSED` would invent a
  failure.

## Success

- A mission can be created, assigned, approved, started and finished on real
  data, with the state visible on screen at each step.
- The map shows the patrol area, the planned route, the ordered checkpoints and
  which of them are visited, missed or undecided.
- Coverage, unpatrolled segments and undecided checkpoints are three distinct
  readings on screen.
- A checkpoint pass is only ever credited to a fresh observation, and the
  screen can show which observation credited it.

## Failure outcomes to exercise

| Failure | Expected |
|---|---|
| Checkpoint placed outside the patrol area | Rejected on approval with the offending checkpoint named |
| Approval with no checkpoint or no assigned unit | Rejected, mission stays `DRAFT` |
| Start before approval | Rejected, mission stays `DRAFT` |
| Second overlapping assignment for one unit | Rejected, existing assignment kept |
| Transition on a terminal mission | Rejected |
| Abort without a reason | Rejected, mission keeps its current state |
| Repeated create request (same idempotency key) | One mission, second request returns the first |
| Two concurrent transitions on one mission | The one built on the stale version is rejected, the other stands |
| Only stale observations during the window | Checkpoints are `UNKNOWN`, coverage does not rise, nothing is reported as missed |
| PostgreSQL unavailable | Writes return 503, `/health/live` stays 200 |

## Out of scope for this slice

- Python simulator driving unit movement (Day 05–06). Mission progress is
  demonstrated by appending observations with a fixture tool under
  `database/tools/`, the same way `refresh_fixture.sql` keeps the current
  fixture usable.
- Control lease, command dispatch and approval authority (Day 07–08).
- Permission matrix for who may approve or start (Day 12).
- Drawing or editing the patrol area polygon itself; the seeded area is used.
- Automatic route generation and per-sensor freshness timestamps.

## Findings from the UI draft

The mock screens (step 2) raised five points the domain has to answer in
step 3. They are recorded here rather than left in the mock.

1. **A fourth checkpoint value is needed.** `PLANNED` means the mission has not
   run yet, so no judgement exists. It is not a coverage verdict and must not
   be counted as coverage or as unpatrolled.
2. **`READY → ABORTED` is a real transition.** An approved mission that has not
   started can still be called off. It was missing from the transition table
   above and has been added.
3. **Closing a mission does not turn silence into a miss.** When a mission ends
   with checkpoints still unjudged, they become `UNKNOWN` unless trustworthy
   observations covered the window. Reporting them as `MISSED` would invent a
   failure the evidence does not support.
4. **A draft needs assignment editing.** The mock only assigns units while
   creating the mission, but the planner has to be able to add or remove an
   assignment on an existing draft before approval.
5. **Coverage percentages do not sum to 100.** While checkpoints are `PLANNED`
   or `UNKNOWN`, the three readings deliberately leave a gap. The screen shows
   that gap instead of normalising it away.

## Verification plan

| What | Where |
|---|---|
| Mission and assignment transitions, rejections | `internal/patrolmission/domain` table-driven tests |
| Coverage judgement including the `UNKNOWN` case | same package, driven by observation fixtures |
| Containment and overlap queries | `adapter` tests against the real database |
| Contract | JSON Schema in `contracts/`, validated from both the Go and console sides as in the previous slice |
| Screen states | console tests for draft, ready, active, completed, aborted, plus loading, empty and error |
| Representative failure | stale-observation run that leaves coverage unchanged |
