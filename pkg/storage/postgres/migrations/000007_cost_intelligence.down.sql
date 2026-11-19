-- 000007_cost_intelligence.down.sql
DROP TABLE IF EXISTS cost_anomalies;
DROP INDEX IF EXISTS idx_cost_snapshots_dimensions;
DROP INDEX IF EXISTS idx_cost_snapshots_account;
DROP INDEX IF EXISTS idx_cost_snapshots_region;
ALTER TABLE cost_snapshots DROP COLUMN IF EXISTS region;
