INSERT INTO site_profiles (id, name, center)
VALUES (
    '10000000-0000-0000-0000-000000000001',
    'Sentinel Test Site',
    ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)')
);

INSERT INTO patrol_areas (id, site_profile_id, name, boundary)
VALUES (
    '20000000-0000-0000-0000-000000000001',
    '10000000-0000-0000-0000-000000000001',
    'Primary Patrol Area',
    ST_GeogFromText('SRID=4326;POLYGON((126.965 37.558,126.991 37.558,126.991 37.575,126.965 37.575,126.965 37.558))')
);

INSERT INTO patrol_units (id, callsign, display_name) VALUES
    ('30000000-0000-0000-0000-000000000001', 'SG-01', 'Sentinel One'),
    ('30000000-0000-0000-0000-000000000002', 'SG-02', 'Sentinel Two'),
    ('30000000-0000-0000-0000-000000000003', 'SG-03', 'Sentinel Three'),
    ('30000000-0000-0000-0000-000000000004', 'SG-04', 'Sentinel Four');

INSERT INTO patrol_unit_snapshots (
    patrol_unit_id, position, battery_level, link_quality, status, observed_at, received_at
) VALUES
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 82, 94, 'NORMAL', now() - interval '5 seconds', now()),
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 61, 72, 'STALE', now() - interval '20 minutes', now() - interval '20 minutes'),
    ('30000000-0000-0000-0000-000000000003', ST_GeogFromText('SRID=4326;POINT(126.9730 37.5645)'), NULL, 88, 'MISSING', now() - interval '10 seconds', now()),
    ('30000000-0000-0000-0000-000000000004', NULL, 37, 0, 'MISSING', NULL, now() - interval '40 minutes');


-- Observation history behind each snapshot. Observation time and receipt time
-- are stored separately so STALE and MISSING states can be derived later.
INSERT INTO patrol_unit_observations (
    patrol_unit_id, position, battery_level, link_quality, observed_at, received_at
) VALUES
    -- SG-01 reports normally.
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9775 37.5661)'), 84, 95, now() - interval '35 seconds', now() - interval '35 seconds'),
    ('30000000-0000-0000-0000-000000000001', ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 82, 94, now() - interval '5 seconds', now()),
    -- SG-02 stopped reporting 20 minutes ago, so its position is stale.
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9812 37.5679)'), 64, 80, now() - interval '25 minutes', now() - interval '25 minutes'),
    ('30000000-0000-0000-0000-000000000002', ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 61, 72, now() - interval '20 minutes', now() - interval '20 minutes'),
    -- SG-03 keeps reporting but its battery telemetry is missing.
    ('30000000-0000-0000-0000-000000000003', ST_GeogFromText('SRID=4326;POINT(126.9730 37.5645)'), NULL, 88, now() - interval '10 seconds', now()),
    -- SG-04 lost its link: a late packet arrived without an observation time.
    ('30000000-0000-0000-0000-000000000004', NULL, 37, 0, NULL, now() - interval '40 minutes');
