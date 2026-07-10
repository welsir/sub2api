ALTER TABLE users
    ADD COLUMN IF NOT EXISTS weekly_cost_threshold NUMERIC(20,8);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS weekly_threshold_notified_week VARCHAR(10);

COMMENT ON COLUMN users.weekly_cost_threshold IS 'Weekly actual_cost alert threshold; Saturday is the first day';
COMMENT ON COLUMN users.weekly_threshold_notified_week IS 'Start date of the last natural week whose alert was claimed';
