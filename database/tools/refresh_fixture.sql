-- Brings the Operational Picture fixture forward in time.
--
-- The seed records absolute timestamps taken from now() when the database was
-- first initialised, so after a few minutes every observation reads as STALE
-- and the fixture stops demonstrating anything. This shifts all observation
-- and receipt times by the age of the newest row, which preserves the relative
-- ages the fixture encodes: SG-01 fresh, SG-02 holding a twenty-minute-old
-- position next to a fresh battery, SG-04 forty minutes silent.
--
-- Run it against a running container:
--   docker compose exec -T postgres \
--     psql -U sentinel -d sentinel_grid -f /sentinel-sql/tools/refresh_fixture.sql

BEGIN;

-- Every sensor has its own pair of times since Day 05-06, so all of them move
-- by the same shift and the relative gaps between sensors survive.
UPDATE patrol_unit_snapshots AS snapshot
SET position_observed_at = snapshot.position_observed_at + shift.amount,
    position_received_at = snapshot.position_received_at + shift.amount,
    battery_observed_at = snapshot.battery_observed_at + shift.amount,
    battery_received_at = snapshot.battery_received_at + shift.amount,
    link_observed_at = snapshot.link_observed_at + shift.amount,
    link_received_at = snapshot.link_received_at + shift.amount,
    session_started_at = snapshot.session_started_at + shift.amount,
    last_received_at = snapshot.last_received_at + shift.amount,
    updated_at = now()
FROM (SELECT now() - max(last_received_at) AS amount FROM patrol_unit_snapshots) AS shift
WHERE shift.amount IS NOT NULL;

UPDATE patrol_unit_observations AS observation
SET observed_at = observation.observed_at + shift.amount,
    received_at = observation.received_at + shift.amount
FROM (SELECT now() - max(received_at) AS amount FROM patrol_unit_observations) AS shift
WHERE shift.amount IS NOT NULL;

UPDATE telemetry_receipts AS receipt
SET observed_at = receipt.observed_at + shift.amount,
    received_at = receipt.received_at + shift.amount
FROM (SELECT now() - max(received_at) AS amount FROM telemetry_receipts) AS shift
WHERE shift.amount IS NOT NULL;

COMMIT;

SELECT unit.callsign,
       round(extract(epoch FROM now() - snapshot.position_observed_at))::int AS position_age_s,
       round(extract(epoch FROM now() - snapshot.battery_observed_at))::int AS battery_age_s,
       snapshot.battery_level,
       snapshot.missing_frames,
       snapshot.rejected_frames,
       round(extract(epoch FROM now() - snapshot.last_received_at))::int AS silent_for_s
FROM patrol_units unit
LEFT JOIN patrol_unit_snapshots snapshot ON snapshot.patrol_unit_id = unit.id
ORDER BY unit.callsign;
