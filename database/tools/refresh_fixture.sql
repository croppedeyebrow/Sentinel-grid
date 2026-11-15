-- Brings the Operational Picture fixture forward in time.
--
-- The seed records absolute timestamps taken from now() when the database was
-- first initialised, so after a few hours every observation reads as STALE and
-- the fixture stops demonstrating anything. This shifts all observation and
-- receipt times by the age of the newest row, which preserves the relative
-- ages the fixture encodes: SG-01 fresh, SG-02 twenty minutes behind, SG-04
-- forty minutes without a position.
--
-- Run it against a running container:
--   docker compose exec -T postgres \
--     psql -U sentinel -d sentinel_grid -f /sentinel-sql/tools/refresh_fixture.sql

BEGIN;

UPDATE patrol_unit_snapshots AS snapshot
SET observed_at = snapshot.observed_at + shift.amount,
    received_at = snapshot.received_at + shift.amount,
    updated_at = now()
FROM (SELECT now() - max(received_at) AS amount FROM patrol_unit_snapshots) AS shift
WHERE shift.amount IS NOT NULL;

UPDATE patrol_unit_observations AS observation
SET observed_at = observation.observed_at + shift.amount,
    received_at = observation.received_at + shift.amount
FROM (SELECT now() - max(received_at) AS amount FROM patrol_unit_observations) AS shift
WHERE shift.amount IS NOT NULL;

COMMIT;

SELECT unit.callsign,
       round(extract(epoch FROM now() - snapshot.observed_at))::int AS observed_seconds_ago,
       snapshot.battery_level,
       snapshot.link_quality
FROM patrol_units unit
LEFT JOIN patrol_unit_snapshots snapshot ON snapshot.patrol_unit_id = unit.id
ORDER BY unit.callsign;
