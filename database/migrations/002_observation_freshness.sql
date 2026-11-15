-- Operational Picture, Day 01-02.
--
-- Unit condition and observation quality are two separate axes, so the
-- projection stores observed values with their timestamps and lets the domain
-- derive freshness and status at read time. The thresholds that decide those
-- derivations belong to the Site Profile, because city and industrial sites do
-- not share one policy.

ALTER TABLE site_profiles
    ADD COLUMN position_stale_after_seconds integer NOT NULL DEFAULT 60,
    ADD COLUMN battery_stale_after_seconds integer NOT NULL DEFAULT 180,
    ADD COLUMN link_stale_after_seconds integer NOT NULL DEFAULT 180,
    ADD COLUMN battery_warning_percent smallint NOT NULL DEFAULT 40,
    ADD COLUMN battery_critical_percent smallint NOT NULL DEFAULT 20,
    ADD COLUMN link_warning_percent smallint NOT NULL DEFAULT 50,
    ADD COLUMN link_critical_percent smallint NOT NULL DEFAULT 20,
    ADD CONSTRAINT site_profiles_stale_windows_positive CHECK (
        position_stale_after_seconds > 0
        AND battery_stale_after_seconds > 0
        AND link_stale_after_seconds > 0
    ),
    ADD CONSTRAINT site_profiles_battery_thresholds CHECK (
        battery_critical_percent BETWEEN 0 AND 100
        AND battery_warning_percent BETWEEN 0 AND 100
        AND battery_critical_percent <= battery_warning_percent
    ),
    ADD CONSTRAINT site_profiles_link_thresholds CHECK (
        link_critical_percent BETWEEN 0 AND 100
        AND link_warning_percent BETWEEN 0 AND 100
        AND link_critical_percent <= link_warning_percent
    );

-- A patrol unit is operated under exactly one Site Profile, which is what
-- resolves the freshness policy for its observations.
ALTER TABLE patrol_units
    ADD COLUMN site_profile_id uuid NOT NULL REFERENCES site_profiles(id);

CREATE INDEX patrol_units_site_profile_idx ON patrol_units (site_profile_id);

-- Planned patrol path drawn on the operational picture. Mission planning and
-- coverage rules arrive with the Patrol Mission slice; this table only carries
-- the geometry the map needs.
CREATE TABLE patrol_routes (
    id uuid PRIMARY KEY,
    patrol_area_id uuid NOT NULL REFERENCES patrol_areas(id),
    name text NOT NULL,
    path geography(LineString, 4326) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX patrol_routes_area_idx ON patrol_routes (patrol_area_id);

-- The stored status mixed unit condition with data quality. Both are derived
-- from the observation timestamps instead, so the column is removed.
ALTER TABLE patrol_unit_snapshots
    DROP COLUMN status;
