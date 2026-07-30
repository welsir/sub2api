-- [INPUT]: Existing users and user_subscriptions tables.
-- [OUTPUT]: Auditable user_activation_journeys lifecycle storage and lookup indexes.
-- [POS]: Persistence boundary for HVOY activation observation; it does not grant benefits or decide eligibility.
--
-- [PROTOCOL]:
-- 1. This migration is append-only and must not backfill historical users.
-- 2. Lifecycle states remain observational; callers must re-evaluate current evidence transactionally.

CREATE TABLE IF NOT EXISTS user_activation_journeys (
    id                                BIGSERIAL PRIMARY KEY,
    user_id                           BIGINT NOT NULL UNIQUE,
    campaign_source                   VARCHAR(64) NOT NULL DEFAULT 'direct',
    starter_state                     VARCHAR(20) NOT NULL DEFAULT 'pending',
    starter_subscription_id           BIGINT,
    starter_granted_at                TIMESTAMPTZ,
    starter_expires_at                TIMESTAMPTZ,
    recall_state                      VARCHAR(20) NOT NULL DEFAULT 'locked',
    recall_subscription_id            BIGINT,
    recall_claimed_at                 TIMESTAMPTZ,
    recall_expires_at                 TIMESTAMPTZ,
    first_success_at                  TIMESTAMPTZ,
    no_attempt_email_sent_at          TIMESTAMPTZ,
    attempted_email_sent_at           TIMESTAMPTZ,
    paid_support_email_sent_at        TIMESTAMPTZ,
    recall_available_email_sent_at    TIMESTAMPTZ,
    recall_expired_email_sent_at      TIMESTAMPTZ,
    last_email_sent_at                TIMESTAMPTZ,
    last_evaluated_at                 TIMESTAMPTZ,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_activation_journeys_user_fk
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT user_activation_journeys_starter_subscription_fk
        FOREIGN KEY (starter_subscription_id) REFERENCES user_subscriptions(id) ON DELETE SET NULL,
    CONSTRAINT user_activation_journeys_recall_subscription_fk
        FOREIGN KEY (recall_subscription_id) REFERENCES user_subscriptions(id) ON DELETE SET NULL,
    CONSTRAINT user_activation_journeys_starter_state_check
        CHECK (starter_state IN ('pending', 'granted', 'expired')),
    CONSTRAINT user_activation_journeys_recall_state_check
        CHECK (recall_state IN ('locked', 'claimable', 'claimed', 'expired', 'blocked_paid', 'closed_success'))
);

CREATE INDEX IF NOT EXISTS idx_user_activation_journeys_campaign_created_at
    ON user_activation_journeys (campaign_source, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_activation_journeys_starter_expires_at
    ON user_activation_journeys (starter_expires_at)
    WHERE starter_state = 'granted' AND starter_expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_activation_journeys_recall_state
    ON user_activation_journeys (recall_state);
