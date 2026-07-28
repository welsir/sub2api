// [INPUT]: Embedded SQL migrations exposed through FS.
// [OUTPUT]: Regression coverage for the user activation journey persistence contract.
// [POS]: Guards migration 177 against missing constraints, indexes, or unsafe backfills.
//
// [PROTOCOL]:
// 1. Update this header when the migration contract changes.
// 2. Keep assertions aligned with the immutable migration and activation plan.
package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserActivationJourneyMigrationDefinesAuditableLifecycleStorage(t *testing.T) {
	content, err := FS.ReadFile("177_add_user_activation_journeys.sql")
	require.NoError(t, err)

	sql := string(content)
	normalizedSQL := strings.Join(strings.Fields(sql), " ")
	require.Contains(t, normalizedSQL, "CREATE TABLE IF NOT EXISTS user_activation_journeys")
	require.Contains(t, normalizedSQL, "user_id BIGINT NOT NULL UNIQUE")
	require.Contains(t, normalizedSQL, "FOREIGN KEY (user_id) REFERENCES users(id)")
	require.Contains(t, normalizedSQL, "FOREIGN KEY (starter_subscription_id) REFERENCES user_subscriptions(id)")
	require.Contains(t, normalizedSQL, "FOREIGN KEY (recall_subscription_id) REFERENCES user_subscriptions(id)")
	require.Contains(t, normalizedSQL, "CHECK (starter_state IN ('pending', 'granted', 'expired'))")
	require.Contains(t, normalizedSQL, "CHECK (recall_state IN ('locked', 'claimable', 'claimed', 'expired', 'blocked_paid', 'closed_success'))")
	require.Contains(t, normalizedSQL, "idx_user_activation_journeys_campaign_created_at")
	require.Contains(t, normalizedSQL, "ON user_activation_journeys (campaign_source, created_at DESC)")
	require.Contains(t, normalizedSQL, "idx_user_activation_journeys_starter_expires_at")
	require.Contains(t, normalizedSQL, "ON user_activation_journeys (starter_expires_at)")
	require.Contains(t, normalizedSQL, "idx_user_activation_journeys_recall_state")
	require.Contains(t, normalizedSQL, "ON user_activation_journeys (recall_state)")

	upperSQL := strings.ToUpper(normalizedSQL)
	require.NotContains(t, upperSQL, "INSERT INTO USER_ACTIVATION_JOURNEYS")
	require.NotContains(t, upperSQL, "UPDATE USER_ACTIVATION_JOURNEYS")
	require.NotContains(t, upperSQL, "INSERT INTO USERS")
	require.NotContains(t, upperSQL, "UPDATE USERS")
}
