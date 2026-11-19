-- 000005_relationship_provenance.down.sql
DROP INDEX IF EXISTS idx_rel_provenance;

ALTER TABLE resource_relationships
DROP COLUMN IF EXISTS confidence,
DROP COLUMN IF EXISTS provenance;
