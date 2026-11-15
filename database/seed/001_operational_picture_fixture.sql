-- Operational Picture fixture.
--
-- The six units cover both axes the console has to separate: unit condition
-- (NORMAL, WARNING, CRITICAL, UNKNOWN) and observation freshness per field
-- (FRESH, STALE, MISSING). Status is never stored; it is derived from these
-- observations and the Site Profile policy at read time.

INSERT INTO site_profiles (
    id, name, center,
    position_stale_after_seconds, battery_stale_after_seconds, link_stale_after_seconds,
    battery_warning_percent, battery_critical_percent,
    link_warning_percent, link_critical_percent
)
VALUES (
    '10000000-0000-0000-0000-000000000001',
    'Sentinel Test Site',
    ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'),
    -- Windows wide enough for the fixture to stay demonstrative for a few
    -- minutes after initialisation, while SG-02 (20 minutes behind) and SG-04
    -- (no observation time) stay stale and missing.
    300, 600, 600,
    40, 20,
    50, 20
);

INSERT INTO patrol_areas (id, site_profile_id, name, boundary)
VALUES (
    '20000000-0000-0000-0000-000000000001',
    '10000000-0000-0000-0000-000000000001',
    'Primary Patrol Area',
    ST_GeogFromText('SRID=4326;POLYGON((126.965 37.558,126.991 37.558,126.991 37.575,126.965 37.575,126.965 37.558))')
);

INSERT INTO patrol_routes (id, patrol_area_id, name, path)
VALUES (
    '40000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    'Riverside Loop',
    ST_GeogFromText('SRID=4326;LINESTRING(126.9690 37.5600,126.9760 37.5620,126.9850 37.5660,126.9880 37.5720,126.9760 37.5735,126.9690 37.5600)')
);

INSERT INTO patrol_units (id, site_profile_id, callsign, display_name) VALUES
    ('30000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'SG-01', 'Sentinel One'),
    ('30000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'SG-02', 'Sentinel Two'),
    ('30000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'SG-03', 'Sentinel Three'),
    ('30000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'SG-04', 'Sentinel Four'),
    ('30000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', 'SG-05', 'Sentinel Five'),
    ('30000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000001', 'SG-06', 'Sentinel Six');

-- Latest projection per unit. observed_at is when the unit measured the value,
-- received_at is when the platform accepted it; both are kept so a late packet
-- is never shown as a current position.
INSERT INTO patrol_unit_snapshots (
    patrol_unit_id, position, battery_level, link_quality, observed_at, received_at
) VALUES
    -- Everything fresh and healthy.
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 82, 94, now() - interval '5 seconds', now()),
    -- Reported 20 minutes ago: every field is stale, so the condition is unknown.
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 61, 72, now() - interval '20 minutes', now() - interval '20 minutes'),
    -- Still reporting, but battery telemetry is absent.
    ('30000000-0000-0000-0000-000000000003', ST_GeogFromText('SRID=4326;POINT(126.9730 37.5645)'), NULL, 88, now() - interval '10 seconds', now()),
    -- Link lost: a late packet arrived with no observation time and no position.
    ('30000000-0000-0000-0000-000000000004', NULL, 37, 0, NULL, now() - interval '40 minutes'),
    -- Fresh observations with a battery level below the site warning threshold.
    ('30000000-0000-0000-0000-000000000005', ST_GeogFromText('SRID=4326;POINT(126.9700 37.5700)'), 35, 88, now() - interval '8 seconds', now()),
    -- Fresh observations with a battery level below the site critical threshold.
    ('30000000-0000-0000-0000-000000000006', ST_GeogFromText('SRID=4326;POINT(126.9860 37.5705)'), 14, 62, now() - interval '6 seconds', now());

-- Observation history behind each snapshot.
INSERT INTO patrol_unit_observations (
    patrol_unit_id, position, battery_level, link_quality, observed_at, received_at
) VALUES
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9775 37.5661)'), 84, 95, now() - interval '35 seconds', now() - interval '35 seconds'),
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 82, 94, now() - interval '5 seconds', now()),
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9812 37.5679)'), 64, 80, now() - interval '25 minutes', now() - interval '25 minutes'),
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 61, 72, now() - interval '20 minutes', now() - interval '20 minutes'),
    ('30000000-0000-0000-0000-000000000003', ST_GeogFromText('SRID=4326;POINT(126.9730 37.5645)'), NULL, 88, now() - interval '10 seconds', now()),
    ('30000000-0000-0000-0000-000000000004', NULL, 37, 0, NULL, now() - interval '40 minutes'),
    ('30000000-0000-0000-0000-000000000005', ST_GeogFromText('SRID=4326;POINT(126.9700 37.5700)'), 35, 88, now() - interval '8 seconds', now()),
    ('30000000-0000-0000-0000-000000000006', ST_GeogFromText('SRID=4326;POINT(126.9860 37.5705)'), 14, 62, now() - interval '6 seconds', now());
