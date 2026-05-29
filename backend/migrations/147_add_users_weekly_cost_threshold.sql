-- Add per-user weekly spending threshold (natural week, Saturday as the first day).
-- weekly_cost_threshold: 用户本自然周 actual_cost 累计达到该值时告警（邮件通知用户与管理员），
--   不阻断请求。NULL 或 <=0 表示不限制。
-- weekly_threshold_notified_week: 上次已告警的自然周起始日(YYYY-MM-DD)，
--   用于保证每个自然周最多告警一次。
ALTER TABLE users ADD COLUMN IF NOT EXISTS weekly_cost_threshold NUMERIC(20,8);
ALTER TABLE users ADD COLUMN IF NOT EXISTS weekly_threshold_notified_week VARCHAR(10);

COMMENT ON COLUMN users.weekly_cost_threshold IS '用户周花费阈值(自然周,周六为第一天)；NULL/<=0 表示不限制。';
COMMENT ON COLUMN users.weekly_threshold_notified_week IS '上次周阈值告警的自然周起始日(YYYY-MM-DD)，用于每周去重。';
