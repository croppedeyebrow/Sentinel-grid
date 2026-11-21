-- Patrol Mission, Day 03-04.
--
-- Checkpoints and assignments belong to a mission rather than to the patrol
-- area: approval freezes the plan to the mission version, so they cannot be
-- edited from elsewhere afterwards.
--
-- The invariants the aggregate enforces are repeated here as constraints where
-- the database can express them, so a bug in the service cannot persist a
-- mission that the domain would reject.

CREATE TABLE missions (
    id uuid PRIMARY KEY,
    site_profile_id uuid NOT NULL REFERENCES site_profiles(id),
    patrol_area_id uuid NOT NULL REFERENCES patrol_areas(id),
    patrol_route_id uuid REFERENCES patrol_routes(id),
    name text NOT NULL CHECK (length(btrim(name)) > 0),
    status text NOT NULL CHECK (status IN ('DRAFT', 'READY', 'ACTIVE', 'COMPLETED', 'ABORTED')),
    version integer NOT NULL DEFAULT 0 CHECK (version >= 0),
    -- Site Profile revision pinned at approval.
    policy_version integer NOT NULL DEFAULT 0 CHECK (policy_version >= 0),
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    abort_reason text CHECK (abort_reason IN ('OPERATOR', 'BATTERY', 'LINK', 'SAFETY', 'POLICY')),
    aborted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    approved_at timestamptz,
    started_at timestamptz,
    ended_at timestamptz,
    CONSTRAINT missions_window_order CHECK (window_end > window_start),
    -- An abort is recorded completely or not at all.
    CONSTRAINT missions_abort_recorded_together CHECK ((abort_reason IS NULL) = (aborted_at IS NULL)),
    -- An abort without a reason is not an abort.
    CONSTRAINT missions_aborted_needs_reason CHECK (status <> 'ABORTED' OR abort_reason IS NOT NULL),
    -- A mission cannot be running or finished without having been approved.
    CONSTRAINT missions_running_needs_approval CHECK (
        status IN ('DRAFT', 'ABORTED') OR approved_at IS NOT NULL
    )
);

CREATE INDEX missions_site_created_idx ON missions (site_profile_id, created_at DESC);
CREATE INDEX missions_status_idx ON missions (status);

CREATE TABLE mission_checkpoints (
    id uuid PRIMARY KEY,
    mission_id uuid NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence > 0),
    name text NOT NULL CHECK (length(btrim(name)) > 0),
    position geography(Point, 4326) NOT NULL,
    radius_meters double precision NOT NULL CHECK (radius_meters > 0 AND radius_meters <= 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (mission_id, sequence)
);

CREATE INDEX mission_checkpoints_mission_idx ON mission_checkpoints (mission_id, sequence);

CREATE TABLE mission_assignments (
    mission_id uuid NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    patrol_unit_id uuid NOT NULL REFERENCES patrol_units(id),
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (mission_id, patrol_unit_id),
    CONSTRAINT mission_assignments_window_order CHECK (window_end > window_start)
);

-- Overlapping-window lookups for one unit drive the assignment conflict check.
CREATE INDEX mission_assignments_unit_window_idx
    ON mission_assignments (patrol_unit_id, window_start, window_end);

-- Repeating a create request with the same key returns the mission created the
-- first time instead of planning a second one.
CREATE TABLE mission_idempotency_keys (
    key text PRIMARY KEY,
    mission_id uuid NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
