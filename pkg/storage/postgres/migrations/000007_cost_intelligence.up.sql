-- 000007_cost_intelligence.up.sql
-- Cost intelligence indexes, dimension support, and cost anomaly tracking with Row-Level Security

ALTER TABLE cost_snapshots ADD COLUMN IF NOT EXISTS region VARCHAR(64) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_cost_snapshots_region ON cost_snapshots(tenant_id, region);
CREATE INDEX IF NOT EXISTS idx_cost_snapshots_account ON cost_snapshots(tenant_id, cloud_account_id);
CREATE INDEX IF NOT EXISTS idx_cost_snapshots_dimensions ON cost_snapshots USING GIN (dimensions);

-- Cost anomalies table
CREATE TABLE IF NOT EXISTS cost_anomalies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cloud_account_id UUID REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    anomaly_date DATE NOT NULL,
    dimension_type VARCHAR(64) NOT NULL, -- 'service', 'account', 'region', 'total'
    dimension_value VARCHAR(255) NOT NULL,
    actual_amount NUMERIC(14, 4) NOT NULL,
    expected_amount NUMERIC(14, 4) NOT NULL,
    deviation_amount NUMERIC(14, 4) NOT NULL,
    percentage_surge NUMERIC(8, 2) NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'medium',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    evidence JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cost_anomalies_tenant ON cost_anomalies(tenant_id, anomaly_date DESC);
CREATE INDEX IF NOT EXISTS idx_cost_anomalies_severity ON cost_anomalies(tenant_id, severity, status);

ALTER TABLE cost_anomalies ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_anomalies FORCE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'cost_anomalies_isolation') THEN
        CREATE POLICY cost_anomalies_isolation ON cost_anomalies
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid)
            WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;
END $$;
