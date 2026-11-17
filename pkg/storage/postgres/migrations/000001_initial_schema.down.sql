-- 000001_initial_schema.down.sql
-- Rollback initial schema and drop RLS tables

DROP TABLE IF EXISTS audit_logs CASCADE;
DROP TABLE IF EXISTS action_executions CASCADE;
DROP TABLE IF EXISTS action_plans CASCADE;
DROP TABLE IF EXISTS ownership_records CASCADE;
DROP TABLE IF EXISTS resource_relationships CASCADE;
DROP TABLE IF EXISTS resource_entities CASCADE;
DROP TABLE IF EXISTS cloud_accounts CASCADE;
DROP TABLE IF EXISTS tenant_users CASCADE;
DROP TABLE IF EXISTS tenants CASCADE;
