-- Store the latest user-authored prompt for each eligible Omni request.

CREATE TABLE IF NOT EXISTS prompt_audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    request_id   VARCHAR(64) NOT NULL DEFAULT '',
    user_id      BIGINT NOT NULL,
    api_key_id   BIGINT NOT NULL,
    group_id     BIGINT,
    endpoint     VARCHAR(128) NOT NULL DEFAULT '',
    protocol     VARCHAR(64) NOT NULL DEFAULT '',
    model        VARCHAR(100) NOT NULL DEFAULT '',
    prompt_text  TEXT NOT NULL,
    prompt_chars INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_created_at
    ON prompt_audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_user_created_at
    ON prompt_audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_api_key_created_at
    ON prompt_audit_logs(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_logs_request_id
    ON prompt_audit_logs(request_id);
