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

	entmigrate "github.com/Wei-Shaw/sub2api/ent/migrate"
	"github.com/stretchr/testify/require"
)

func TestUserActivationJourneyMigrationDefinesAuditableLifecycleStorage(t *testing.T) {
	content, err := FS.ReadFile("177_add_user_activation_journeys.sql")
	require.NoError(t, err)

	sql := string(content)
	normalizedSQL := strings.Join(strings.Fields(sql), " ")
	require.Contains(t, normalizedSQL, "CREATE TABLE IF NOT EXISTS user_activation_journeys")
	require.Contains(t, normalizedSQL, "user_id BIGINT NOT NULL UNIQUE")
	require.Contains(t, normalizedSQL, "campaign_source VARCHAR(64) NOT NULL DEFAULT 'direct'")
	require.Contains(t, normalizedSQL, "starter_state VARCHAR(20) NOT NULL DEFAULT 'pending'")
	require.Contains(t, normalizedSQL, "recall_state VARCHAR(20) NOT NULL DEFAULT 'locked'")
	require.Contains(t, normalizedSQL, "created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()")
	require.Contains(t, normalizedSQL, "updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()")
	for _, nullableColumn := range []string{
		"starter_subscription_id BIGINT",
		"starter_granted_at TIMESTAMPTZ",
		"starter_expires_at TIMESTAMPTZ",
		"recall_subscription_id BIGINT",
		"recall_claimed_at TIMESTAMPTZ",
		"recall_expires_at TIMESTAMPTZ",
		"first_success_at TIMESTAMPTZ",
		"no_attempt_email_sent_at TIMESTAMPTZ",
		"attempted_email_sent_at TIMESTAMPTZ",
		"paid_support_email_sent_at TIMESTAMPTZ",
		"recall_available_email_sent_at TIMESTAMPTZ",
		"recall_expired_email_sent_at TIMESTAMPTZ",
		"last_email_sent_at TIMESTAMPTZ",
		"last_evaluated_at TIMESTAMPTZ",
	} {
		require.Contains(t, normalizedSQL, nullableColumn+",")
		require.NotContains(t, normalizedSQL, nullableColumn+" NOT NULL")
	}
	require.Contains(t, normalizedSQL, "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE")
	require.Contains(t, normalizedSQL, "FOREIGN KEY (starter_subscription_id) REFERENCES user_subscriptions(id) ON DELETE SET NULL")
	require.Contains(t, normalizedSQL, "FOREIGN KEY (recall_subscription_id) REFERENCES user_subscriptions(id) ON DELETE SET NULL")
	require.Contains(t, normalizedSQL, "CHECK (starter_state IN ('pending', 'granted', 'expired'))")
	require.Contains(t, normalizedSQL, "CHECK (recall_state IN ('locked', 'claimable', 'claimed', 'expired', 'blocked_paid', 'closed_success'))")
	require.Contains(t, normalizedSQL, "idx_user_activation_journeys_campaign_created_at")
	require.Contains(t, normalizedSQL, "ON user_activation_journeys (campaign_source, created_at DESC)")
	require.Contains(t, normalizedSQL, "idx_user_activation_journeys_starter_expires_at")
	require.Contains(t, normalizedSQL, "ON user_activation_journeys (starter_expires_at)")
	require.Contains(t, normalizedSQL, "WHERE starter_state = 'granted' AND starter_expires_at IS NOT NULL")
	require.Contains(t, normalizedSQL, "idx_user_activation_journeys_recall_state")
	require.Contains(t, normalizedSQL, "ON user_activation_journeys (recall_state)")

	upperSQL := strings.ToUpper(normalizedSQL)
	require.NotContains(t, upperSQL, "INSERT INTO USER_ACTIVATION_JOURNEYS")
	require.NotContains(t, upperSQL, "UPDATE USER_ACTIVATION_JOURNEYS")
	require.NotContains(t, upperSQL, "INSERT INTO USERS")
	require.NotContains(t, upperSQL, "UPDATE USERS")
}

func TestUserActivationJourneyEntIndexesKeepCampaignCreatedAtDescending(t *testing.T) {
	for _, idx := range entmigrate.UserActivationJourneysTable.Indexes {
		if idx.Name != "useractivationjourney_campaign_source_created_at" {
			continue
		}
		require.NotNil(t, idx.Annotation)
		require.True(t, idx.Annotation.DescColumns["created_at"])
		return
	}
	require.Fail(t, "campaign source and created-at index not generated")
}

func TestUserActivationJourneyEntIndexesKeepStarterExpiryPredicate(t *testing.T) {
	for _, idx := range entmigrate.UserActivationJourneysTable.Indexes {
		if idx.Name != "useractivationjourney_starter_expires_at" {
			continue
		}
		require.NotNil(t, idx.Annotation)
		require.Equal(t, "starter_state = 'granted' AND starter_expires_at IS NOT NULL", idx.Annotation.Where)
		return
	}
	require.Fail(t, "starter expiry index not generated")
}
