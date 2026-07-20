# Prompt 审计落库设计

## 目标

在 Omni 用户版的每次用户主动请求中，记录本轮新增的用户 Prompt，供运营人员后续直接通过 PostgreSQL 查询。

本功能只负责落库，不提供管理后台页面、查询 API、导出能力，也不记录模型输出。

## 分支与交付边界

- 目标分支：`dev/omni`
- 不创建额外功能分支。
- 不修改或同步到 `main`、`dev/team`、`dev/company-01-runtime-merge`。
- 不复用计费表 `usage_logs` 或风控表 `content_moderation_logs`。

## 审计语义

一条审计记录对应一次请求中最新出现的用户消息。

- OpenAI Responses：读取 `input` 中最后一条用户文本消息。
- OpenAI Chat Completions：读取 `messages` 中最后一条 `role=user` 消息。
- Anthropic Messages：读取 `messages` 中最后一条 `role=user` 消息。
- Gemini：读取 `contents` 中最后一条用户文本内容。
- 图片接口：读取顶层 `prompt` 文本。
- WebSocket Responses：按每个用户发起的 `response.create` 请求检查并记录。

如果请求末尾是 assistant 消息、工具调用或工具输出，则不创建 Prompt 审计记录。这样同一用户消息触发的工具续请求不会重复落库。

Prompt 文本保存原始文字内容，不复用现有内容审核的空白归一化和 240 字摘要逻辑。多文本块按原有顺序拼接；图片 URL、图片 Base64、文件内容不写入审计表。

## 数据模型

新增独立表 `prompt_audit_logs`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `BIGSERIAL` | 主键 |
| `request_id` | `VARCHAR(64)` | 当前请求标识 |
| `user_id` | `BIGINT` | 用户 ID |
| `api_key_id` | `BIGINT` | API Key ID |
| `group_id` | `BIGINT NULL` | 分组 ID |
| `endpoint` | `VARCHAR(128)` | 入站接口 |
| `protocol` | `VARCHAR(64)` | 请求协议 |
| `model` | `VARCHAR(100)` | 用户请求的模型 |
| `prompt_text` | `TEXT` | 本轮用户 Prompt 原文 |
| `prompt_chars` | `INTEGER` | Unicode 字符数 |
| `created_at` | `TIMESTAMPTZ` | 接收时间 |

索引仅保留后续数据库拉取最常用的组合：

- `created_at DESC`
- `(user_id, created_at DESC)`
- `(api_key_id, created_at DESC)`
- `request_id`

审计表不建立业务外键，避免用户或 API Key 删除后破坏历史记录，也避免请求路径额外承担级联约束。

## 请求流程

1. 请求完成鉴权并读取请求体。
2. 从请求体中提取最新用户文本。
3. 如果没有新的用户文本，则跳过审计。
4. 将审计写入任务投递到有界异步队列。
5. 原请求继续执行现有路由、计费和上游转发流程。
6. 后台 worker 将记录写入 PostgreSQL。

审计功能不改变请求体，也不参与模型路由和计费决策。

## 失败策略

采用 fail-open：

- 提取失败、队列已满或数据库写入失败时，记录结构化错误日志。
- 不阻断用户请求，不返回新的错误码，不重试上游请求。
- 允许极端情况下漏记少量 Prompt，不提供强一致性保证。

异步队列必须有容量上限，不能因数据库故障无限积压内存。

## 验证范围

- 各协议的单条、历史多轮和多文本块提取测试。
- assistant/tool 续请求不落库测试。
- Prompt 原文与字符数持久化测试。
- 队列满和仓储失败时原请求仍继续的测试。
- HTTP Responses、Chat Completions 和 WebSocket 至少各一条入口集成测试。
- 迁移可在空库和已有线上结构上安全执行。

## 明确不做

- 模型输出审计。
- 管理后台入口或查询接口。
- Prompt 搜索、导出、统计和告警。
- 内容审核、关键词拦截或自动封禁。
- 强一致写入、消息队列或失败补偿。
- 跨业务分支同步。
