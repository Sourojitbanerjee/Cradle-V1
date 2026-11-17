-- 000002_provenance_and_discovery_jobs.up.sql
-- Adds provenance-aware columns to resource_entities and introduces discovery_jobs table

ALTER TABLE resource_entities
    ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS created_at_provider TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS observed_state JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS derived_state JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS inferred_state JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_resource_entities_tags ON resource_entities USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_resource_entities_observed ON resource_entities(tenant_id, observed_at DESC);

-- Discovery Jobs Tracking Table
CREATE TABLE IF NOT EXISTS discovery_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cloud_account_id UUID NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    regions TEXT[] NOT NULL DEFAULT '{}',
    services TEXT[] NOT NULL DEFAULT '{}',
    discovered_count INT NOT NULL DEFAULT 0,
    error_summary JSONB NOT NULL DEFAULT '{}',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_discovery_jobs_tenant ON discovery_jobs(tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_discovery_jobs_account ON discovery_jobs(cloud_account_id, started_at DESC);

-- Enable RLS on discovery_jobs
ALTER TABLE discovery_jobs ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'discovery_jobs_isolation') THEN
        CREATE POLICY discovery_jobs_isolation ON discovery_jobs
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;
END $$;
