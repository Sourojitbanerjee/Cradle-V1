-- 000001_initial_schema.up.sql
-- Core Cradle Multi-Tenant Schema with Active Row-Level Security (RLS)

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- 1. Tenants Table
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(63) NOT NULL UNIQUE,
    plan_tier VARCHAR(32) NOT NULL DEFAULT 'standard',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);

-- 2. Tenant Users Table
CREATE TABLE IF NOT EXISTS tenant_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tenant_user_email UNIQUE (tenant_id, email)
);
CREATE INDEX IF NOT EXISTS idx_tenant_users_tenant ON tenant_users(tenant_id);

-- 3. Cloud Accounts Table
CREATE TABLE IF NOT EXISTS cloud_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    role_arn VARCHAR(512) NOT NULL,
    external_id VARCHAR(128) NOT NULL,
    regions TEXT[] NOT NULL DEFAULT '{"us-east-1"}',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    last_discovered_at TIMESTAMPTZ,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tenant_provider_account UNIQUE (tenant_id, provider, account_id)
);
CREATE INDEX IF NOT EXISTS idx_cloud_accounts_tenant ON cloud_accounts(tenant_id);

-- 4. Resource Entities (The Canonical Inventory)
CREATE TABLE IF NOT EXISTS resource_entities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cloud_account_id UUID NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    region VARCHAR(64) NOT NULL,
    resource_type VARCHAR(128) NOT NULL,
    native_id VARCHAR(512) NOT NULL,
    arn VARCHAR(1024) NOT NULL,
    name VARCHAR(255),
    status VARCHAR(64) NOT NULL,
    configuration JSONB NOT NULL DEFAULT '{}',
    raw_payload JSONB NOT NULL DEFAULT '{}',
    configuration_hash CHAR(64) NOT NULL,
    monthly_cost_estimate NUMERIC(12, 4) DEFAULT 0.0000,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT uq_tenant_resource_arn UNIQUE (tenant_id, arn)
);
CREATE INDEX IF NOT EXISTS idx_resource_entities_lookup ON resource_entities(tenant_id, resource_type, region);
CREATE INDEX IF NOT EXISTS idx_resource_entities_native ON resource_entities(tenant_id, native_id);
CREATE INDEX IF NOT EXISTS idx_resource_entities_config ON resource_entities USING GIN (configuration);

-- 5. Resource Relationships (Dependency Graph Edges)
CREATE TABLE IF NOT EXISTS resource_relationships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    source_resource_id UUID NOT NULL REFERENCES resource_entities(id) ON DELETE CASCADE,
    target_resource_id UUID NOT NULL REFERENCES resource_entities(id) ON DELETE CASCADE,
    relationship_type VARCHAR(64) NOT NULL,
    properties JSONB NOT NULL DEFAULT '{}',
    is_implicit BOOLEAN NOT NULL DEFAULT FALSE,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tenant_relationship UNIQUE (tenant_id, source_resource_id, target_resource_id, relationship_type)
);
CREATE INDEX IF NOT EXISTS idx_rel_source ON resource_relationships(tenant_id, source_resource_id);
CREATE INDEX IF NOT EXISTS idx_rel_target ON resource_relationships(tenant_id, target_resource_id);
CREATE INDEX IF NOT EXISTS idx_rel_type ON resource_relationships(tenant_id, relationship_type);

-- 6. Ownership Records
CREATE TABLE IF NOT EXISTS ownership_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    resource_id UUID NOT NULL REFERENCES resource_entities(id) ON DELETE CASCADE,
    owner_type VARCHAR(32) NOT NULL,
    owner_id VARCHAR(255) NOT NULL,
    source VARCHAR(64) NOT NULL,
    confidence NUMERIC(3, 2) NOT NULL,
    is_verified BOOLEAN NOT NULL DEFAULT FALSE,
    verified_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tenant_resource_owner UNIQUE (tenant_id, resource_id, owner_type, owner_id)
);
CREATE INDEX IF NOT EXISTS idx_ownership_resource ON ownership_records(tenant_id, resource_id);

-- 7. Action Plans
CREATE TABLE IF NOT EXISTS action_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    action_type VARCHAR(64) NOT NULL,
    risk_level VARCHAR(16) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    estimated_monthly_savings NUMERIC(10, 2) DEFAULT 0.00,
    blast_radius_summary JSONB NOT NULL DEFAULT '{}',
    plan_payload JSONB NOT NULL DEFAULT '{}',
    rollback_strategy JSONB NOT NULL DEFAULT '{}',
    created_by VARCHAR(255) NOT NULL,
    approved_by VARCHAR(255),
    approved_at TIMESTAMPTZ,
    rejection_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_action_plans_tenant ON action_plans(tenant_id, status);

-- 8. Action Executions
CREATE TABLE IF NOT EXISTS action_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    action_plan_id UUID NOT NULL REFERENCES action_plans(id) ON DELETE CASCADE,
    step VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    output_log TEXT,
    error_details JSONB,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_action_executions_plan ON action_executions(tenant_id, action_plan_id);

-- 9. Audit Logs (Append-Only Cryptographic Ledger)
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    actor_id VARCHAR(255) NOT NULL,
    actor_type VARCHAR(32) NOT NULL,
    event_action VARCHAR(128) NOT NULL,
    target_type VARCHAR(64) NOT NULL,
    target_id VARCHAR(512) NOT NULL,
    client_ip VARCHAR(45),
    user_agent TEXT,
    details JSONB NOT NULL DEFAULT '{}',
    previous_hash CHAR(64),
    record_hash CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant ON audit_logs(tenant_id, created_at DESC);

-- =========================================================================
-- ROW-LEVEL SECURITY (RLS) POLICIES
-- Ensures mathematical tenant isolation at the PostgreSQL storage engine layer.
-- =========================================================================

ALTER TABLE tenant_users ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE resource_entities ENABLE ROW LEVEL SECURITY;
ALTER TABLE resource_relationships ENABLE ROW LEVEL SECURITY;
ALTER TABLE ownership_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE action_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE action_executions ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    -- tenant_users policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'tenant_users_isolation') THEN
        CREATE POLICY tenant_users_isolation ON tenant_users
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- cloud_accounts policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'cloud_accounts_isolation') THEN
        CREATE POLICY cloud_accounts_isolation ON cloud_accounts
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- resource_entities policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'resource_entities_isolation') THEN
        CREATE POLICY resource_entities_isolation ON resource_entities
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- resource_relationships policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'resource_relationships_isolation') THEN
        CREATE POLICY resource_relationships_isolation ON resource_relationships
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- ownership_records policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'ownership_records_isolation') THEN
        CREATE POLICY ownership_records_isolation ON ownership_records
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- action_plans policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'action_plans_isolation') THEN
        CREATE POLICY action_plans_isolation ON action_plans
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- action_executions policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'action_executions_isolation') THEN
        CREATE POLICY action_executions_isolation ON action_executions
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;

    -- audit_logs policy
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'audit_logs_isolation') THEN
        CREATE POLICY audit_logs_isolation ON audit_logs
            AS RESTRICTIVE
            USING (tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid);
    END IF;
END $$;
