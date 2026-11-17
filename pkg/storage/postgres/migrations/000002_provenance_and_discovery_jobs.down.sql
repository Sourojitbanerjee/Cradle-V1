-- 000002_provenance_and_discovery_jobs.down.sql
DROP TABLE IF EXISTS discovery_jobs CASCADE;

ALTER TABLE resource_entities
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS created_at_provider,
    DROP COLUMN IF EXISTS observed_at,
    DROP COLUMN IF EXISTS observed_state,
    DROP COLUMN IF EXISTS derived_state,
    DROP COLUMN IF EXISTS inferred_state;
