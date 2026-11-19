CREATE TABLE ownership_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    tag_key VARCHAR(128) NOT NULL,
    tag_value VARCHAR(256) NOT NULL,
    priority INT NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 10000),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    assignments JSONB NOT NULL CHECK (jsonb_typeof(assignments) = 'object' AND assignments <> '{}'::jsonb),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ownership_rules_tenant ON ownership_rules(tenant_id, priority DESC);
ALTER TABLE ownership_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE ownership_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY ownership_rules_access ON ownership_rules
    USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
