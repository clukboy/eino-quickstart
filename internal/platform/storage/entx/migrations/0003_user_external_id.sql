-- 0003: stable upstream identity for idempotent user synchronization.
ALTER TABLE users ADD COLUMN IF NOT EXISTS external_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS users_external_id_key ON users (external_id) WHERE external_id IS NOT NULL;
