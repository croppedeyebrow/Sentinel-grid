-- Patrol Mission fixture.
--
-- Windows are written relative to initialisation time so the active mission is
-- actually running, and its first checkpoint sits on SG-01's latest observed
-- position so coverage has something real to credit.
--
-- The completed mission is deliberately older than any stored observation: its
-- checkpoints come out UNKNOWN rather than MISSED, which is the rule that the
-- platform does not call a checkpoint unpatrolled without evidence.

INSERT INTO missions (
    id, site_profile_id, patrol_area_id, patrol_route_id,
    name, status, version, policy_version,
    window_start, window_end,
    created_at, approved_at, started_at, ended_at
) VALUES
    (
        '50000000-0000-0000-0000-000000000001',
        '10000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000001',
        '40000000-0000-0000-0000-000000000001',
        '하천변 정기 순찰 (오전)', 'ACTIVE', 2, 1,
        now() - interval '30 minutes', now() + interval '90 minutes',
        now() - interval '60 minutes', now() - interval '40 minutes', now() - interval '30 minutes', NULL
    ),
    (
        '50000000-0000-0000-0000-000000000002',
        '10000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000001',
        NULL,
        '공사 구역 상황 순찰', 'DRAFT', 0, 0,
        now() + interval '3 hours', now() + interval '4 hours',
        now() - interval '20 minutes', NULL, NULL, NULL
    ),
    (
        '50000000-0000-0000-0000-000000000003',
        '10000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000001',
        '40000000-0000-0000-0000-000000000001',
        '하천변 정기 순찰 (야간)', 'COMPLETED', 4, 1,
        now() - interval '1 day' - interval '2 hours', now() - interval '1 day',
        now() - interval '1 day' - interval '3 hours', now() - interval '1 day' - interval '150 minutes',
        now() - interval '1 day' - interval '2 hours', now() - interval '1 day'
    );

INSERT INTO mission_checkpoints (id, mission_id, sequence, name, position, radius_meters) VALUES
    -- Active mission: CP-1 sits on SG-01's latest observation, so it is passed.
    ('60000000-0000-0000-0000-000000000001', '50000000-0000-0000-0000-000000000001', 1, 'CP-1 하천 진입',
     ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 40),
    ('60000000-0000-0000-0000-000000000002', '50000000-0000-0000-0000-000000000001', 2, 'CP-2 보행교',
     ST_GeogFromText('SRID=4326;POINT(126.9820 37.5685)'), 40),
    ('60000000-0000-0000-0000-000000000003', '50000000-0000-0000-0000-000000000001', 3, 'CP-3 공원 북측',
     ST_GeogFromText('SRID=4326;POINT(126.9700 37.5700)'), 40),

    -- Draft mission: planned, no assignment yet, so approval refuses it.
    ('60000000-0000-0000-0000-000000000011', '50000000-0000-0000-0000-000000000002', 1, 'CP-1 출입 통제',
     ST_GeogFromText('SRID=4326;POINT(126.9860 37.5705)'), 40),
    ('60000000-0000-0000-0000-000000000012', '50000000-0000-0000-0000-000000000002', 2, 'CP-2 자재 적치장',
     ST_GeogFromText('SRID=4326;POINT(126.9805 37.5695)'), 40),

    -- Completed mission: no observation covers its window.
    ('60000000-0000-0000-0000-000000000021', '50000000-0000-0000-0000-000000000003', 1, 'CP-1 하천 진입',
     ST_GeogFromText('SRID=4326;POINT(126.9780 37.5665)'), 40),
    ('60000000-0000-0000-0000-000000000022', '50000000-0000-0000-0000-000000000003', 2, 'CP-2 하천 복귀',
     ST_GeogFromText('SRID=4326;POINT(126.9760 37.5735)'), 40);

INSERT INTO mission_assignments (mission_id, patrol_unit_id, window_start, window_end) VALUES
    ('50000000-0000-0000-0000-000000000001', '30000000-0000-0000-0000-000000000001',
     now() - interval '30 minutes', now() + interval '90 minutes'),
    ('50000000-0000-0000-0000-000000000003', '30000000-0000-0000-0000-000000000005',
     now() - interval '1 day' - interval '2 hours', now() - interval '1 day');
