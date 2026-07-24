ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS provider_pricing_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS provider_pricing_group_name VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS provider_pricing_models JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_provider_pricing_group_name
    ON groups (LOWER(provider_pricing_group_name))
    WHERE deleted_at IS NULL AND provider_pricing_group_name <> '';
