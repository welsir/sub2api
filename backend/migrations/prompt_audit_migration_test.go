package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptAuditMigrationDefinesAppendOnlyStorage(t *testing.T) {
	content, err := FS.ReadFile("174_prompt_audit_logs.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS prompt_audit_logs")
	require.Contains(t, sql, "prompt_text  TEXT NOT NULL")
	require.Contains(t, sql, "prompt_chars INTEGER NOT NULL DEFAULT 0")
	require.Contains(t, sql, "idx_prompt_audit_logs_created_at")
	require.Contains(t, sql, "idx_prompt_audit_logs_user_created_at")
	require.Contains(t, sql, "idx_prompt_audit_logs_api_key_created_at")
	require.Contains(t, sql, "idx_prompt_audit_logs_request_id")
	require.NotContains(t, sql, "REFERENCES users")
	require.NotContains(t, sql, "REFERENCES api_keys")
}
