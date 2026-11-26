-- Telemetry Reliability, Day 05-06.
--
-- Until now a snapshot row carried one observation time for position, battery
-- and link together, so a stale position made a fresh battery look stale. That
-- was recorded as a limitation in ADR 0003 and as requirement TEL-01. Each
-- sensor now carries its own measurement and receipt time.
--
-- The snapshot also gains the state of the unit's stream, because "nothing is
-- being measured" and "nothing is arriving" are different problems and the
-- operator has to be able to tell them apart.

ALTER TABLE patrol_unit_snapshots
    ADD COLUMN position_observed_at timestamptz,
    ADD COLUMN position_received_at timestamptz,
    ADD COLUMN battery_observed_at timestamptz,
    ADD COLUMN battery_received_at timestamptz,
    ADD COLUMN link_observed_at timestamptz,
    ADD COLUMN link_received_at timestamptz,
    -- Stream state. session_id is the connected run the readings came from; a
    -- reconnect changes it without discarding what was already measured.
    ADD COLUMN session_id text,
    ADD COLUMN session_started_at timestamptz,
    ADD COLUMN last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    ADD COLUMN missing_frames bigint NOT NULL DEFAULT 0 CHECK (missing_frames >= 0),
    ADD COLUMN rejected_frames bigint NOT NULL DEFAULT 0 CHECK (rejected_frames >= 0),
    ADD COLUMN last_received_at timestamptz;

-- Carry the single pair over to every sensor so existing rows keep meaning the
-- same thing, then drop it: one time for three sensors is what TEL-01 removes.
UPDATE patrol_unit_snapshots
SET position_observed_at = observed_at,
    position_received_at = received_at,
    battery_observed_at = observed_at,
    battery_received_at = received_at,
    link_observed_at = observed_at,
    link_received_at = received_at,
    last_received_at = received_at;

ALTER TABLE patrol_unit_snapshots
    DROP COLUMN observed_at,
    DROP COLUMN received_at;

-- A value without the moment it was measured cannot be aged, so the pair is
-- kept together: either both or neither.
ALTER TABLE patrol_unit_snapshots
    ADD CONSTRAINT patrol_unit_snapshots_position_times CHECK (
        (position_observed_at IS NULL) = (position_received_at IS NULL)
    ),
    ADD CONSTRAINT patrol_unit_snapshots_battery_times CHECK (
        (battery_observed_at IS NULL) = (battery_received_at IS NULL)
    ),
    ADD CONSTRAINT patrol_unit_snapshots_link_times CHECK (
        (link_observed_at IS NULL) = (link_received_at IS NULL)
    );

-- Observations are the applied history that mission coverage is judged from.
-- They now carry which frame they came from, so a verdict can be traced back
-- to the receipt of the frame that produced it.
ALTER TABLE patrol_unit_observations
    ADD COLUMN session_id text,
    ADD COLUMN sequence bigint CHECK (sequence IS NULL OR sequence > 0);

CREATE INDEX patrol_unit_observations_session_idx
    ON patrol_unit_observations (patrol_unit_id, session_id, sequence);

-- Receipt log: everything that arrived, applied or not, including what was
-- refused. 종합_기획_문서/07_데이터베이스_이벤트_아키텍처.md requires the fact
-- of receipt to survive even when the value is not applied.
--
-- A refused frame may not have parsed, so unit, session and sequence are
-- nullable here while they are required for anything that was applied.
CREATE TABLE telemetry_receipts (
    event_id text PRIMARY KEY,
    patrol_unit_id uuid REFERENCES patrol_units(id),
    session_id text,
    sequence bigint CHECK (sequence IS NULL OR sequence > 0),
    outcome text NOT NULL CHECK (
        outcome IN ('ACCEPTED', 'DUPLICATE', 'OUT_OF_ORDER', 'GAP', 'REJECTED')
    ),
    observed_at timestamptz,
    received_at timestamptz NOT NULL,
    sensors jsonb,
    missing_frames bigint CHECK (missing_frames IS NULL OR missing_frames > 0),
    rejection_code text,
    rejection_detail text,
    frame_bytes integer NOT NULL CHECK (frame_bytes >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    -- A refusal without a reason would be unreadable later.
    CONSTRAINT telemetry_receipts_rejection_recorded CHECK (
        (outcome <> 'REJECTED') = (rejection_code IS NULL)
    ),
    -- Only a gap reports missing frames.
    CONSTRAINT telemetry_receipts_missing_only_on_gap CHECK (
        (outcome = 'GAP') = (missing_frames IS NOT NULL)
    ),
    -- Anything that was applied has to say what it applied to.
    CONSTRAINT telemetry_receipts_applied_is_identified CHECK (
        outcome = 'REJECTED'
        OR (patrol_unit_id IS NOT NULL AND session_id IS NOT NULL
            AND sequence IS NOT NULL AND observed_at IS NOT NULL)
    )
);

CREATE INDEX telemetry_receipts_unit_received_idx
    ON telemetry_receipts (patrol_unit_id, received_at DESC);

CREATE INDEX telemetry_receipts_outcome_idx
    ON telemetry_receipts (outcome, received_at DESC);
