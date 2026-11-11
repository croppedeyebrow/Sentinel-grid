CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE site_profiles (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    center geography(Point, 4326) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE patrol_areas (
    id uuid PRIMARY KEY,
    site_profile_id uuid NOT NULL REFERENCES site_profiles(id),
    name text NOT NULL,
    boundary geography(Polygon, 4326) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE patrol_units (
    id uuid PRIMARY KEY,
    callsign text NOT NULL UNIQUE,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE patrol_unit_observations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    patrol_unit_id uuid NOT NULL REFERENCES patrol_units(id),
    position geography(Point, 4326),
    battery_level smallint CHECK (battery_level BETWEEN 0 AND 100),
    link_quality smallint CHECK (link_quality BETWEEN 0 AND 100),
    observed_at timestamptz,
    received_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX patrol_unit_observations_unit_received_idx
    ON patrol_unit_observations (patrol_unit_id, received_at DESC);

CREATE TABLE patrol_unit_snapshots (
    patrol_unit_id uuid PRIMARY KEY REFERENCES patrol_units(id),
    position geography(Point, 4326),
    battery_level smallint CHECK (battery_level BETWEEN 0 AND 100),
    link_quality smallint CHECK (link_quality BETWEEN 0 AND 100),
    status text NOT NULL CHECK (status IN ('NORMAL', 'STALE', 'MISSING')),
    observed_at timestamptz,
    received_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

