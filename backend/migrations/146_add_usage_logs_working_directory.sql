-- Add per-request working directory (cwd) to usage logs.
-- working_directory: 客户端发起请求时的工作目录。
-- 由 Claude Code 系统提示 <env> 块的 "Working directory:" 行，或 Codex 的
-- <environment_context><cwd>...</cwd> 解析得到；取不到时为 NULL。
-- 用于按目录归集 actual_cost，识别员工是否将 AI 用于非公司项目。
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS working_directory VARCHAR(1024);

COMMENT ON COLUMN usage_logs.working_directory IS '客户端工作目录(cwd)，由请求体解析；NULL 表示未识别。用于按目录归集花费。';
