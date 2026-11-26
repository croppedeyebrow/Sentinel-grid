-- Operational Picture fixture.
--
-- The six units cover both axes the console has to separate: unit condition
-- (NORMAL, WARNING, CRITICAL, UNKNOWN) and observation freshness per sensor
-- (FRESH, STALE, MISSING). Status is never stored; it is derived from these
-- observations and the Site Profile policy at read time.
--
-- Since Day 05-06 each sensor carries its own measurement and receipt time, so
-- SG-02 holds a stale position next to a fresh battery — the case that one
-- shared timestamp could not express.

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
    -- minutes after initialisation, while SG-02's position (20 minutes behind)
    -- and SG-04 (no observation time) stay stale and missing.
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

-- Latest projection per unit. Each sensor keeps the moment it was measured and
-- the moment it was received, so a late packet is never shown as a current
-- reading and a stale sensor never ages a fresh one.
INSERT INTO patrol_unit_snapshots (
    patrol_unit_id, position, battery_level, link_quality,
    position_observed_at, position_received_at,
    battery_observed_at, battery_received_at,
    link_observed_at, link_received_at,
    session_id, session_started_at, last_sequence, missing_frames, rejected_frames, last_received_at
) VALUES
    -- Everything fresh and healthy, stream complete.
    ('30000000-0000-0000-0000-000000000001',
     ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 82, 94,
     now() - interval '5 seconds', now(),
     now() - interval '5 seconds', now(),
     now() - interval '5 seconds', now(),
     'seed-session-sg01', now() - interval '35 minutes', 120, 0, 0, now()),

    -- The case per-sensor freshness exists for: the position is twenty minutes
    -- behind while the battery and link are current. The condition is still
    -- unknown, but the console can now say which sensor went stale. Three
    -- frames were reported missing in this session.
    ('30000000-0000-0000-0000-000000000002',
     ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 61, 72,
     now() - interval '20 minutes', now() - interval '20 minutes',
     now() - interval '6 seconds', now(),
     now() - interval '6 seconds', now(),
     'seed-session-sg02', now() - interval '28 minutes', 88, 3, 0, now()),

    -- Still reporting position and link, but battery telemetry is absent, and
    -- two frames were refused outright.
    ('30000000-0000-0000-0000-000000000003',
     ST_GeogFromText('SRID=4326;POINT(126.9730 37.5645)'), NULL, 88,
     now() - interval '10 seconds', now(),
     NULL, NULL,
     now() - interval '10 seconds', now(),
     'seed-session-sg03', now() - interval '22 minutes', 64, 0, 2, now()),

    -- Link lost forty minutes ago. The battery and link it last reported are
    -- on file and stale; its position was never reported in this session, so
    -- that sensor is missing rather than old. Nothing has arrived since.
    ('30000000-0000-0000-0000-000000000004',
     NULL, 37, 0,
     NULL, NULL,
     now() - interval '40 minutes', now() - interval '40 minutes',
     now() - interval '40 minutes', now() - interval '40 minutes',
     'seed-session-sg04', now() - interval '70 minutes', 51, 7, 0, now() - interval '40 minutes'),

    -- Fresh observations with a battery level below the site warning threshold.
    ('30000000-0000-0000-0000-000000000005',
     ST_GeogFromText('SRID=4326;POINT(126.9700 37.5700)'), 35, 88,
     now() - interval '8 seconds', now(),
     now() - interval '8 seconds', now(),
     now() - interval '8 seconds', now(),
     'seed-session-sg05', now() - interval '18 minutes', 73, 0, 0, now()),

    -- Fresh observations with a battery level below the site critical threshold.
    ('30000000-0000-0000-0000-000000000006',
     ST_GeogFromText('SRID=4326;POINT(126.9860 37.5705)'), 14, 62,
     now() - interval '6 seconds', now(),
     now() - interval '6 seconds', now(),
     now() - interval '6 seconds', now(),
     'seed-session-sg06', now() - interval '12 minutes', 69, 0, 0, now());

-- Observation history behind each snapshot, carrying the frame it came from so
-- a coverage verdict can be traced back to the receipt of that frame.
INSERT INTO patrol_unit_observations (
    patrol_unit_id, position, battery_level, link_quality, observed_at, received_at,
    session_id, sequence
) VALUES
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9775 37.5661)'), 84, 95, now() - interval '35 seconds', now() - interval '35 seconds', 'seed-session-sg01', 119),
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 82, 94, now() - interval '5 seconds', now(), 'seed-session-sg01', 120),
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9812 37.5679)'), 64, 80, now() - interval '25 minutes', now() - interval '25 minutes', 'seed-session-sg02', 80),
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 61, 72, now() - interval '20 minutes', now() - interval '20 minutes', 'seed-session-sg02', 84),
    -- A battery-only frame: the position was not measured, so it is not
    -- reported, and the stored position keeps ageing.
    ('30000000-0000-0000-0000-000000000002', NULL, 61, 72, now() - interval '6 seconds', now(), 'seed-session-sg02', 88),
    ('30000000-0000-0000-0000-000000000003', ST_GeogFromText('SRID=4326;POINT(126.9730 37.5645)'), NULL, 88, now() - interval '10 seconds', now(), 'seed-session-sg03', 64),
    ('30000000-0000-0000-0000-000000000004', NULL, 37, 0, now() - interval '40 minutes', now() - interval '40 minutes', 'seed-session-sg04', 51),
    ('30000000-0000-0000-0000-000000000005', ST_GeogFromText('SRID=4326;POINT(126.9700 37.5700)'), 35, 88, now() - interval '8 seconds', now(), 'seed-session-sg05', 73),
    ('30000000-0000-0000-0000-000000000006', ST_GeogFromText('SRID=4326;POINT(126.9860 37.5705)'), 14, 62, now() - interval '6 seconds', now(), 'seed-session-sg06', 69);

-- Receipt log: what arrived for each unit, including the frames that were
-- refused. SG-03's two refusals are visible here with their reason.
INSERT INTO telemetry_receipts (
    event_id, patrol_unit_id, session_id, sequence, outcome,
    observed_at, received_at, sensors, missing_frames,
    rejection_code, rejection_detail, frame_bytes
) VALUES
    ('seed-receipt-sg01-120', '30000000-0000-0000-0000-000000000001', 'seed-session-sg01', 120, 'ACCEPTED',
     now() - interval '5 seconds', now(),
     '{"position":{"latitude":37.5665,"longitude":126.978},"battery":82,"link":94}', NULL,
     NULL, NULL, 182),
    ('seed-receipt-sg02-88', '30000000-0000-0000-0000-000000000002', 'seed-session-sg02', 88, 'ACCEPTED',
     now() - interval '6 seconds', now(),
     '{"battery":61,"link":72}', NULL,
     NULL, NULL, 131),
    ('seed-receipt-sg02-85', '30000000-0000-0000-0000-000000000002', 'seed-session-sg02', 85, 'GAP',
     now() - interval '12 minutes', now() - interval '12 minutes',
     '{"battery":62,"link":70}', 3,
     NULL, NULL, 131),
    ('seed-receipt-sg03-63', '30000000-0000-0000-0000-000000000003', 'seed-session-sg03', 63, 'REJECTED',
     NULL, now() - interval '3 minutes', NULL, NULL,
     'VALUE_OUT_OF_RANGE', 'battery is out of range: 150', 166),
    ('seed-receipt-sg03-62', '30000000-0000-0000-0000-000000000003', 'seed-session-sg03', 62, 'REJECTED',
     NULL, now() - interval '4 minutes', NULL, NULL,
     'MALFORMED_FRAME', 'frame does not match the schema: trailing characters', 154),
    ('seed-receipt-sg04-51', '30000000-0000-0000-0000-000000000004', 'seed-session-sg04', 51, 'ACCEPTED',
     now() - interval '40 minutes', now() - interval '40 minutes',
     '{"battery":37,"link":0}', NULL,
     NULL, NULL, 129);
