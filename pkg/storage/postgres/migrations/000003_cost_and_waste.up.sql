-- 000003_cost_and_waste.up.sql
-- Ingestion tables for cost telemetry, metric rollups, and waste findings with RLS

-- 1. Cost Snapshots Table
CREATE TABLE IF NOT EXISTS cost_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cloud_account_id UUID NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    resource_id UUID REFERENCES resource_entities(id) ON DELETE SET NULL,
    resource_arn VARCHAR(1024),
    service_name VARCHAR(128) NOT NULL,
    usage_type VARCHAR(255) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    amount NUMERIC(14, 4) NOT NULL DEFAULT 0.0000,
    currency VARCHAR(16) NOT NULL DEFAULT 'USD',
    usage_quantity NUMERIC(16, 4) NOT NULL DEFAULT 0.0000,
    usage_unit VARCHAR(64) NOT NULL DEFAULT '',
    dimensions JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cost_snapshots_period ON cost_snapshots(tenant_id, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_cost_snapshots_service ON cost_snapshots(tenant_id, service_name);
CREATE INDEX IF NOT EXISTS idx_cost_snapshots_resource ON cost_snapshots(tenant_id, resource_id);

-- 2. Metric Rollups Table
CREATE TABLE IF NOT EXISTS metric_rollups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    resource_id UUID NOT NULL REFERENCES resource_entities(id) ON DELETE CASCADE,
    metric_name VARCHAR(128) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    avg_value NUMERIC(14, 4) NOT NULL DEFAULT 0.0000,
    p95_value NUMERIC(14, 4) NOT NULL DEFAULT 0.0000,
    max_value NUMERIC(14, 4) NOT NULL DEFAULT 0.0000,
    min_value NUMERIC(14, 4) NOT NULL DEFAULT 0.0000,
    sample_count INT NOT NULL DEFAULT 0,
    unit VARCHAR(32) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_metric_rollups_lookup ON metric_rollups(tenant_id, resource_id, metric_name, period_start DESC);

-- 3. Waste Findings Table
CREATE TABLE IF NOT EXISTS waste_findings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cloud_account_id UUID NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    resource_id UUID NOT NULL REFERENCES resource_entities(id) ON DELETE CASCADE,
    resource_arn VARCHAR(1024) NOT NULL,
    resource_type VARCHAR(128) NOT NULL,
    resource_name VARCHAR(255) NOT NULL DEFAULT '',
    region VARCHAR(64) NOT NULL DEFAULT '',
    waste_type VARCHAR(64) NOT NULL,
    confidence NUMERIC(3, 2) NOT NULL DEFAULT 1.00,
    estimated_monthly_savings NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    observation_period_days INT NOT NULL DEFAULT 0,
    facts JSONB NOT NULL DEFAULT '[]',
    inference_reason TEXT NOT NULL DEFAULT '',
    recommendation_summary TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tenant_resource_waste UNIQUE (tenant_id, resource_id, waste_type)
);
CREATE INDEX IF NOT EXISTS idx_waste_findings_tenant ON waste_findings(tenant_id, status, waste_type);
CREATE INDEX IF NOT EXISTS idx_waste_findings_savings ON waste_findings(tenant_id, estimated_monthly_savings DESC);

-- =========================================================================
-- ROW-LEVEL SECURITY (RLS) POLICIES
-- =========================================================================

ALTER TABLE cost_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE metric_rollups ENABLE ROW LEVEL SECURITY;
ALTER TABLE waste_findings ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'cost_snapshots_isolation') THEN
        CREATE POLICY cost_snapshots_isolation ON cost_snapshots
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'metric_rollups_isolation') THEN
        CREATE POLICY metric_rollups_isolation ON metric_rollups
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'waste_findings_isolation') THEN
        CREATE POLICY waste_findings_isolation ON waste_findings
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;
END $$;
