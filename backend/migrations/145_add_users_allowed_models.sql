-- Add per-user model whitelist.
-- allowed_models: 用户级模型白名单（JSON 字符串数组，支持通配符如 "claude-*"）。
-- 为空数组表示不限制，放行全部模型。
-- 在网关入口处做准入校验（请求被禁模型时返回 403），并同步过滤 GET /v1/models 展示列表。
ALTER TABLE users ADD COLUMN IF NOT EXISTS allowed_models JSONB DEFAULT '[]'::jsonb;

COMMENT ON COLUMN users.allowed_models IS '用户级模型白名单（支持通配符）；空数组表示不限制，放行全部模型。';
