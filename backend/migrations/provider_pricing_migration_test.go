package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderPricingMigrationAddsDynamicPublishedGroupFields(t *testing.T) {
	content, err := FS.ReadFile("176_add_group_provider_pricing.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS provider_pricing_enabled BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS provider_pricing_group_name VARCHAR(100) NOT NULL DEFAULT ''")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS provider_pricing_models JSONB NOT NULL DEFAULT '[]'::jsonb")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_provider_pricing_group_name")
	require.Contains(t, sql, "LOWER(provider_pricing_group_name)")
	require.Contains(t, sql, "WHERE deleted_at IS NULL AND provider_pricing_group_name <> ''")
}
