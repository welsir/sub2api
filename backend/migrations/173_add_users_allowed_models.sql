-- Add per-user model whitelist.
-- Empty arrays mean unrestricted access; entries support trailing wildcards.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN users.allowed_models IS 'Per-user model whitelist; empty means unrestricted';
