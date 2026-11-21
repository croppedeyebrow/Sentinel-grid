-- Approval freezes the plan to a policy revision, which requires the Site
-- Profile to have one. The number is bumped whenever the observation policy
-- (freshness windows, battery and link thresholds) changes, so a mission
-- records which policy its judgements were made under.

ALTER TABLE site_profiles
    ADD COLUMN policy_version integer NOT NULL DEFAULT 1 CHECK (policy_version > 0);
