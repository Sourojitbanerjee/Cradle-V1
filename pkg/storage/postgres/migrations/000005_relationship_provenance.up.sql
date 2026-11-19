-- 000005_relationship_provenance.up.sql
-- Add confidence and provenance columns to resource_relationships for epistemological transparency

ALTER TABLE resource_relationships 
ADD COLUMN IF NOT EXISTS confidence NUMERIC(4, 2) NOT NULL DEFAULT 1.00,
ADD COLUMN IF NOT EXISTS provenance VARCHAR(32) NOT NULL DEFAULT 'OBSERVED_FROM_PROVIDER';

CREATE INDEX IF NOT EXISTS idx_rel_provenance ON resource_relationships(tenant_id, provenance);
