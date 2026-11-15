# ADR 0002: Unit condition and observation freshness are separate axes

## Status

Accepted — Day 01–02, Operational Picture slice.

## Context

The first fixture exposed a single `status` field with the values
`NORMAL`, `STALE` and `MISSING`. That one field mixed two different questions:

- Is the unit all right? (`NORMAL`)
- Can the data be trusted? (`STALE`, `MISSING`)

The planning documents require these to stay apart.
`종합_기획_문서/06_React_관제UI.md` states that
`NORMAL/WARNING/CRITICAL/UNKNOWN` and `FRESH/STALE/PARTIAL/INVALID` are to be
separated, `종합_기획_문서/10_도메인모델_요구사항.md` requires keeping
`battery=70%` and `battery freshness=STALE` together, and the project's first
design principle is to separate unit state from data quality.

The collapsed field also could not express what was wrong. A unit reporting a
fresh position with no battery telemetry and a unit that had lost its link
entirely both came out as `MISSING`, which hid the difference between
"something is missing" and "we cannot see the unit at all".

## Decision

1. **Two fields.** Every unit carries `status`
   (`NORMAL`, `WARNING`, `CRITICAL`, `UNKNOWN`) and, separately, a freshness
   verdict per observed field (`FRESH`, `STALE`, `MISSING`) on position,
   battery and link.
2. **Status is derived, never stored.** `patrol_unit_snapshots` holds observed
   values with `observed_at` and `received_at`; migration `002` dropped the
   stored `status` column. The domain derives both axes at read time, so a
   verdict can never be older than the observation behind it.
3. **Insufficient evidence outranks everything.** If any required field is not
   `FRESH`, the condition is `UNKNOWN`. A stale 5% battery is not reported as
   `CRITICAL`, and a stale healthy battery is not reported as `NORMAL`. This
   follows invariant 5 in `10_도메인모델_요구사항.md`.
4. **Thresholds belong to the Site Profile.** Staleness windows and the
   warning/critical percentages are columns on `site_profiles` and are
   published in the API response, because city and industrial sites do not
   share one policy (design principle 6).
5. **Untrusted values stay visible.** A stale or missing observation keeps its
   last value and both timestamps. The console shows the last known position
   marked as not current rather than moving it or hiding it, as
   `프로젝트_관련_기술_정리/04_React_TypeScript_GIS.md` requires.

## Consequences

- The response is larger: each field became an object with value, two
  timestamps and a freshness verdict. In exchange no client has to guess why a
  value is unreliable.
- The console renders two axes. It explains a verdict with the observations or
  thresholds that produced it, and never recomputes one itself.
- One moment is used per response (`generatedAt`), so every unit in a response
  is judged against the same clock.
- `FRESH` is the term used in code and contracts. The old `NORMAL/STALE/MISSING`
  enum is gone; `종합_기획_문서/01_종합_기획서.md` still writes
  `KNOWN/STALE/PARTIAL/UNKNOWN` for the same axis, which should be reconciled
  when the Telemetry Reliability slice introduces `PARTIAL` and `INVALID`.

## Limitations

- One snapshot row carries a single `observed_at` and `received_at` for all
  three fields, so a field is `MISSING` only when its value is absent — the
  three fields cannot yet age independently. Per-sensor timestamps arrive with
  the Telemetry Reliability slice (TEL-01).
- `WARNING` and `CRITICAL` are derived from battery and link percentages only.
  Mission feasibility (return distance, route, payload) is a separate judgement
  and is not part of this slice.
- The seed records absolute timestamps, so the fixture ages. `database/tools/refresh_fixture.sql`
  shifts it forward for local demonstrations.
