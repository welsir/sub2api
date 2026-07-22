# WgetCloud 账号粘性稳定池实施计划

## 实现方向

该能力作为 Sub2API 公司分支功能实现。Sub2API 负责账号发现、健康状态、稳定池计算、代理绑定和管理员控制；Mihomo 仅作为内部多通道代理执行器。

## 阶段 1：纯逻辑与测试

1. 在 `backend/internal/service` 增加 WgetCloud pool 领域模型。
2. 测试并实现元数据节点过滤、三成功恢复、两失败摘除、30 分钟隔离和延迟 EWMA。
3. 测试并实现最多 5 节点的稳定池选择，稳定性优先于延迟。
4. 测试并实现基于 SHA-256 的 rendezvous hashing。
5. 验证相同输入映射稳定、移除 lane 只迁移受影响账号、不同服务器盐值产生不同分布。

建议文件：

- `backend/internal/service/wgetcloud_pool.go`
- `backend/internal/service/wgetcloud_pool_test.go`

## 阶段 2：Mihomo 适配器

1. 增加内部 Mihomo controller client，读取 provider 节点和 lane selector。
2. controller 地址、provider 名称、lane 名称及端口全部来自公司环境配置。
3. 支持读取节点、选择 lane 节点、读取当前选择和执行 lane canary。
4. 禁止 controller 访问外部公开地址；禁止记录订阅 URL 或节点凭据。

建议文件：

- `backend/internal/service/mihomo_pool_client.go`
- `backend/internal/service/mihomo_pool_client_test.go`

## 阶段 3：持久状态

增加公司分支迁移，持久化：

- pool 节点健康计数、EWMA、隔离时间；
- lane 到节点及 Sub2API proxy ID 的映射；
- lane pin 及其过期时间；
- 最近一次优化时间。

不持久化 OAuth token、代理密码、订阅 URL 或响应正文。账号分配从当前 `proxy_id` 和一致性哈希动态推导，不另存敏感映射。

建议文件：

- `backend/migrations/176_wgetcloud_account_sticky_pool.sql`
- `backend/internal/repository/wgetcloud_pool_repo.go`
- `backend/internal/repository/wgetcloud_pool_repo_test.go`

## 阶段 4：账号与探测

1. 通过现有账号 repository 获取当前有效 OpenAI OAuth 账号，不固定 ID。
2. 复用现有账号状态和代理服务层，不直接更新账号表。
3. 使用 `gpt-5.4-mini` 发送最小 `hi` Responses 流式请求。
4. 账号范围错误换账号重试；路径范围错误处罚节点。
5. 每次迁移记录旧 `proxy_id`，更新后重新读取并 canary，失败自动恢复。

## 阶段 5：后台调度器

1. 增加公司环境开关，默认关闭。
2. 每 2 分钟探测所有 occupied lane 和一个非生产候选。
3. 先生成 decision plan，审计模式只记录匿名摘要，不执行写操作。
4. 执行模式一次最多优化替换一个健康 lane；故障 lane 可立即处理。
5. 使用单实例锁防止多个副本同时调度。

建议文件：

- `backend/internal/service/wgetcloud_pool_scheduler.go`
- `backend/internal/service/wgetcloud_pool_scheduler_test.go`
- `backend/internal/config/config.go`
- `backend/internal/service/wire.go`

## 阶段 6：管理员控制面

增加公司管理员接口：

- 查询候选健康、稳定池、lane 和匿名账号分布；
- pin/unpin 指定 lane；
- 切换 audit/active 模式；
- 手动触发单节点探测；
- 查看最近迁移与回滚结果。

接口不得返回 OAuth token、代理凭据或原始账号标识。

建议文件：

- `backend/internal/handler/admin/wgetcloud_pool_handler.go`
- `backend/internal/handler/admin/wgetcloud_pool_handler_test.go`
- `backend/internal/server/routes.go`

## 阶段 7：Mihomo 与代理记录安装

1. 在39隔离环境验证 5 个 listener，每个绑定独立 lane selector。
2. listener 只对内部 Docker 网络开放。
3. 创建 5 个 Sub2API 内部 HTTP proxy 记录，但不绑定任何账号。
4. 每条 lane 使用 `gpt-5.4-mini` 完成 HTTP 200 + SSE 验证。
5. 保留代理 ID 5 与 `新加坡 03` 原路径。

## 阶段 8：灰度上线

1. 冻结39当前 selector、账号代理绑定、容器状态和错误率基线。
2. audit 模式完成至少一轮 29 节点探测。
3. 动态选择一个当前可调度账号迁移，观察 10 分钟。
4. 逐个迁移其余符合条件账号。
5. 三个稳定周期无迁移后启用后台执行模式。
6. 停用旧双跳自动晋升任务。

## 验证命令门槛

本地至少执行：

```bash
cd backend
go test ./internal/service/... ./internal/repository/... ./internal/handler/admin/...
go test ./...
```

生产验收必须记录：

- Sub2API 健康与重启数；
- 29 个候选、稳定池和 lane 状态；
- `gpt-5.4-mini` 完整探测；
- 账号迁移前后 proxy ID；
- WAF 403、TLS、EOF、超时和 5xx 对比；
- 回滚演练结果。

## 提交边界

仅提交到 `dev/company-01-runtime-merge`。不向 `main`、`dev/omni` 或 `dev/team` 搬运。SQL 迁移、后端代码、测试、部署配置和运行记录按可验证的小提交分阶段进入。
