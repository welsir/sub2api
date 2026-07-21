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

func TestUpstreamAuditMigrationDefinesShanghaiDailyPartitionsAndCompressedPayloads(t *testing.T) {
	content, err := FS.ReadFile("175_upstream_audit_logs.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS public.upstream_audit_logs")
	require.Contains(t, sql, "PARTITION BY RANGE (created_at)")
	require.Contains(t, sql, "request_body_zstd BYTEA")
	require.Contains(t, sql, "response_body_zstd BYTEA")
	require.Contains(t, sql, "request_sha256 VARCHAR(64)")
	require.Contains(t, sql, "response_sha256 VARCHAR(64)")
	require.Contains(t, sql, "response_complete BOOLEAN")
	require.Contains(t, sql, "ensure_upstream_audit_partition")
	require.Contains(t, sql, "AT TIME ZONE 'Asia/Shanghai'")
	require.Contains(t, sql, "pg_advisory_xact_lock")
	require.Contains(t, sql, "refusing to recreate closed upstream audit partition")
	require.Contains(t, sql, "upstream_audit_outcome_valid")
	require.Contains(t, sql, "upstream_audit_request_sizes_valid")
	require.NotContains(t, sql, "JSONB NOT NULL")
}
