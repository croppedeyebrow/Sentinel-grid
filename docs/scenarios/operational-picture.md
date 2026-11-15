# Operational Picture scenario

## User and goal

A patrol operator opens the console at the start of a shift and has to answer
two questions for every unit on the site: is the unit all right, and can the
data on screen be trusted. The two answers are shown separately — a unit can be
healthy while its telemetry is stale, and the console must say so rather than
collapse both into one label. See `docs/adr/0002-two-axis-unit-status.md`.

## Starting conditions

- One Site Profile ("Sentinel Test Site") with its own staleness windows and
  battery/link thresholds.
- One patrol area boundary and one planned route drawn under the units.
- Six patrol units covering both axes.

## Fixture

| Unit | Condition | Observation quality | What it demonstrates |
|---|---|---|---|
| SG-01 | `NORMAL` | position, battery, link `FRESH` | Everything current and within thresholds |
| SG-02 | `UNKNOWN` | all three `STALE` (20 minutes behind) | Stale data is not evidence of a healthy or failing unit |
| SG-03 | `UNKNOWN` | position and link `FRESH`, battery `MISSING` | A single missing sensor blocks the verdict, and the map still shows a current position |
| SG-04 | `UNKNOWN` | all three `MISSING`, no observation time | Lost link: the last received values survive but are not trusted, and the unit cannot be placed on the map |
| SG-05 | `WARNING` | all `FRESH` | Battery below the site warning threshold |
| SG-06 | `CRITICAL` | all `FRESH` | Battery below the site critical threshold |

## Expected behaviour

1. The console distinguishes `NORMAL`, `WARNING`, `CRITICAL` and `UNKNOWN` on
   the condition axis, and `FRESH`, `STALE`, `MISSING` per field on the quality
   axis.
2. Every card explains its verdict with the observation or the site threshold
   that produced it, and no verdict is recomputed in the browser.
3. A position that is not fresh is drawn as a dashed marker with its age, never
   moved or hidden.
4. The map shows the patrol area and the planned route beneath the units.
5. `loading`, `empty` (a site with no units), and `error` (API unreachable or a
   response that breaks the contract) each have their own state on screen.

## Representative failure

Stopping PostgreSQL must keep `GET /health/live` at 200 while
`GET /health/ready` and `GET /api/v1/operational-picture` return 503, and the
console must show its error state with a retry. The API recovers without a
restart once the database is back.

## Verification

- Domain rules: `services/mission-api/internal/operationalpicture/domain`
- Service behaviour: `.../application/service_test.go`
- Contract (API side): `.../adapter/http_test.go` validates the response and
  the committed example against `contracts/schemas/operational-picture.schema.json`
- Contract (console side): `apps/console/src/tests/contract.test.ts` parses the
  same published example
- UI states: `apps/console/src/tests/OperationalPicturePage.test.tsx`
