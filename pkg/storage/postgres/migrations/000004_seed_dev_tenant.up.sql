-- 000004_seed_dev_tenant.up.sql
-- Seeds default local development tenant for testing and initial environment bootstrap

INSERT INTO tenants (id, name, slug, plan_tier, status, created_at, updated_at)
VALUES (
    'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11',
    'Default Development Tenant',
    'default-dev',
    'enterprise',
    'active',
    NOW(),
    NOW()
)
ON CONFLICT (id) DO NOTHING;
