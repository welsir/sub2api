# 43 V2 Single-Hop Stable Pool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 `dev/omni` 为 43 V2 实现单 Mihomo、五条生产 Lane、动态 OAuth 探测、一致性账号亲和和安全自动切换，并以 2 小时观察期和代理 ID `6` 完成可回滚上线。

**Architecture:** Sub2API 内置稳定池控制器，PostgreSQL 保存节点、Lane、探测和审计状态；单 Mihomo 容器暴露 5 条生产 Lane、3 条候选探测 Lane和 1 条 Canary Lane。账号 `proxy_id` 是唯一出口事实，V2 应用容器不设置全局代理环境变量。

**Tech Stack:** Go 1.26, Ent, PostgreSQL, Gin, Vue 3, Vitest, Docker Compose, Mihomo REST API

---

## 实施约束

- 只在 `dev/omni` 实施，不同步到 `main`、公司或团队分支。
- 保留 V2 当前代理 ID `6`，作为全局回滚目标。
- 保留并避开工作树中已有的 `deploy/omni-v2/`、计费代码和部署计划改动。
- 每个代码任务先写失败测试，再写最小实现。
- 本计划提交后不自动进入开发或生产操作。

### Task 1: 固化领域模型和配置契约

**Files:**
- Create: `backend/internal/service/proxy_pool.go`
- Create: `backend/internal/service/proxy_pool_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

**Steps:**

1. 写失败测试，覆盖默认 `observe`、5 个生产 Lane、3 个候选探测 Lane、1 个 Canary Lane、2 分钟探测、99% 成功率、3 连胜、P95 2.5 秒、明显退化 5 秒、2 次失败摘除、3 个慢窗口、20% 性能优势、30 分钟普通替换冷却、2 小时观察、30 分钟 Canary，以及必须配置回滚代理 ID。
2. 运行：

   ```bash
   cd backend && go test ./internal/config ./internal/service -run 'TestProxyPool(Config|Defaults|Thresholds)' -count=1
   ```

   Expected: FAIL，稳定池类型尚不存在。
3. 定义 `ProxyPoolConfig`、`ProxyPoolMode`、`NodeState`、`LaneState`、`ProbeOutcome`、`FailureClass` 和配置校验。
4. 重跑相同命令，Expected: PASS。
5. 提交：

   ```bash
   git add backend/internal/config backend/internal/service/proxy_pool.go backend/internal/service/proxy_pool_test.go
   git commit -m "feat(omni): define stable proxy pool contract"
   ```

### Task 2: 增加持久化与迁移

**Files:**
- Create: `backend/ent/schema/proxy_pool_node.go`
- Create: `backend/ent/schema/proxy_pool_lane.go`
- Create: `backend/ent/schema/proxy_pool_probe.go`
- Create: `backend/ent/schema/proxy_pool_audit.go`
- Create: `backend/migrations/176_proxy_stable_pool.sql`
- Create: `backend/migrations/proxy_stable_pool_migration_test.go`
- Create: `backend/internal/repository/proxy_pool_repo.go`
- Create: `backend/internal/repository/proxy_pool_repo_test.go`
- Regenerate: `backend/ent/*`

**Steps:**

1. 写失败测试，验证 provider/name 唯一节点、Lane 号唯一、期望与实际节点、固定状态、探测证据、迁移审计和历史保留。
2. 运行：

   ```bash
   cd backend && go test ./migrations ./internal/repository -run 'TestProxyStablePool(Migration|Repository)' -count=1
   ```

   Expected: FAIL。
3. 实现 Ent、SQL 和 repository。时间字段用 `timestamptz`，探测表按 `node_id, observed_at` 建索引，不修改现有 `accounts.proxy_id` 语义。
4. 生成并验证：

   ```bash
   cd backend && make generate
   go test ./migrations ./internal/repository -run 'TestProxyStablePool(Migration|Repository)' -count=1
   ```

   Expected: PASS。
5. 提交：

   ```bash
   git add backend/ent backend/migrations backend/internal/repository/proxy_pool_repo.go backend/internal/repository/proxy_pool_repo_test.go
   git commit -m "feat(omni): persist stable proxy pool state"
   ```

### Task 3: 实现 Mihomo 客户端与节点发现

**Files:**
- Create: `backend/internal/repository/mihomo_client.go`
- Create: `backend/internal/repository/mihomo_client_test.go`
- Create: `backend/internal/service/proxy_pool_discovery.go`
- Create: `backend/internal/service/proxy_pool_discovery_test.go`

**Steps:**

1. 用 `httptest.Server` 写失败测试：读取 provider、稳定节点键、过滤套餐/流量/重置/提示伪节点、读取策略组实际节点、切换后回读、超时或不一致时返回不确定状态。
2. 运行：

   ```bash
   cd backend && go test ./internal/repository ./internal/service -run 'Test(MihomoClient|ProxyPoolDiscovery)' -count=1
   ```

   Expected: FAIL。
3. 实现短超时、有限响应体、secret 日志脱敏和切换回读；控制器不可达时保持数据面，不盲切。
4. 重跑相同命令，Expected: PASS。
5. 提交：

   ```bash
   git add backend/internal/repository/mihomo_client* backend/internal/service/proxy_pool_discovery*
   git commit -m "feat(omni): discover and control mihomo lanes"
   ```

### Task 4: 实现动态 OAuth 与匿名传输探针

**Files:**
- Create: `backend/internal/service/proxy_pool_probe.go`
- Create: `backend/internal/service/proxy_pool_probe_test.go`
- Modify: `backend/internal/service/account_test_service.go`
- Modify: `backend/internal/service/account_test_service_openai_test.go`

**Steps:**

1. 写失败测试：2 个账号用不同账号复核；1 个账号用新连接重试加匿名探针；0 个账号只输出“证据不足”。
2. 补充测试：`gpt-5.4-mini` 流式 `hi` 必须 HTTP 200、合法 SSE 与有效完成；401/403/429 不扣节点分；匿名 401 表示传输可达；timeout/TLS/EOF/502/503 是传输失败；每轮最多 5 个生产加 3 个候选并发，轮次不叠加。
3. 运行：

   ```bash
   cd backend && go test ./internal/service -run 'TestProxyPoolProbe' -count=1
   ```

   Expected: FAIL。
4. 复用现有 OpenAI/SSE 路径，允许测试服务显式传入 Lane proxy URL；探测不修改账号 `proxy_id`，每轮重新查询账号。
5. 运行：

   ```bash
   cd backend && go test ./internal/service -run 'TestProxyPoolProbe|TestAccountTestServiceOpenAI' -count=1
   ```

   Expected: PASS。
6. 提交：

   ```bash
   git add backend/internal/service/proxy_pool_probe* backend/internal/service/account_test_service.go backend/internal/service/account_test_service_openai_test.go
   git commit -m "feat(omni): probe proxy nodes with dynamic oauth evidence"
   ```

### Task 5: 实现评分、Top 5 与安全切换

**Files:**
- Create: `backend/internal/service/proxy_pool_score.go`
- Create: `backend/internal/service/proxy_pool_score_test.go`
- Create: `backend/internal/service/proxy_pool_controller.go`
- Create: `backend/internal/service/proxy_pool_controller_test.go`

**Steps:**

1. 写失败测试：99%、3 连胜、P95 2.5 秒晋升门槛；稳定优先于平均延迟；2 次失败立即摘除；慢 3 个窗口；候选快 20%；30 分钟仅限制性能替换；首次晋升须 30 分钟 Canary；无合格替代时回滚 ID `6`；`observe` 只记录，`auto` 才执行。
2. 运行：

   ```bash
   cd backend && go test ./internal/service -run 'TestProxyPool(Score|Controller|Failover|Canary)' -count=1
   ```

   Expected: FAIL。
3. 实现幂等状态机。切换流程固定为：读取旧选择器、写新选择器、回读、业务探针；不确定时审计并恢复旧选择器或进入保护模式。
4. 重跑相同命令，Expected: PASS。
5. 提交：

   ```bash
   git add backend/internal/service/proxy_pool_score* backend/internal/service/proxy_pool_controller*
   git commit -m "feat(omni): rank and fail over stable proxy lanes"
   ```

### Task 6: 实现账号亲和与逐账号回滚

**Files:**
- Create: `backend/internal/service/proxy_pool_affinity.go`
- Create: `backend/internal/service/proxy_pool_affinity_test.go`
- Modify: `backend/internal/repository/account_repo.go`
- Modify: `backend/internal/repository/account_repo_test.go`

**Steps:**

1. 写失败测试：只处理有效 OpenAI OAuth；账号增删时最小迁移；Lane 集合不变保持亲和；单账号可用；零账号不迁移；Lane 故障只重算受影响账号；迁移失败恢复旧代理；全局保护回滚 ID `6`；影子账号继续继承母账号代理。
2. 运行：

   ```bash
   cd backend && go test ./internal/service ./internal/repository -run 'TestProxyPoolAffinity' -count=1
   ```

   Expected: FAIL。
3. 使用已有 `xxhash` 实现 Rendezvous Hash。迁移通过 repository 事务和 scheduler cache invalidation，不直接写 SQL 绕过缓存。
4. 重跑相同命令，Expected: PASS。
5. 提交：

   ```bash
   git add backend/internal/service/proxy_pool_affinity* backend/internal/repository/account_repo.go backend/internal/repository/account_repo_test.go
   git commit -m "feat(omni): keep oauth accounts sticky to healthy lanes"
   ```

### Task 7: 接入生命周期、管理 API 与 Wire

**Files:**
- Create: `backend/internal/handler/admin/proxy_pool_handler.go`
- Create: `backend/internal/handler/admin/proxy_pool_handler_test.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire.go`
- Regenerate: `backend/cmd/server/wire_gen.go`

**Steps:**

1. 写失败测试覆盖：状态、节点、Lane、账号分配、模式切换、手动测试、固定/解除固定、审计 API；验证管理员鉴权，固定 Lane 在明确故障时仍可撤离。
2. 端点统一放在 `/api/v1/admin/proxy-pool/*`。
3. 运行：

   ```bash
   cd backend && go test ./internal/handler/admin ./internal/server/routes -run 'TestProxyPool' -count=1
   ```

   Expected: FAIL。
4. 实现控制器启动/停止，默认 `observe`；关闭时停止调度并等待在途探针。
5. 生成并测试：

   ```bash
   cd backend && make generate
   go test ./internal/handler/admin ./internal/server/routes -run 'TestProxyPool' -count=1
   ```

   Expected: PASS。
6. 提交：

   ```bash
   git add backend/internal/handler/admin/proxy_pool_handler* backend/internal/server/routes/admin.go backend/internal/handler/wire.go backend/internal/service/wire.go backend/cmd/server/wire*
   git commit -m "feat(omni): expose stable proxy pool controls"
   ```

### Task 8: 增加后台管理页面

**Files:**
- Create: `frontend/src/api/admin/proxyPool.ts`
- Create: `frontend/src/views/admin/ProxyPoolView.vue`
- Create: `frontend/src/views/admin/__tests__/ProxyPoolView.spec.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/i18n/locales/zh-CN/admin/proxies.ts`
- Modify: `frontend/src/i18n/locales/en/admin/proxies.ts`

**Steps:**

1. 写失败测试：展示全部节点、Top 5、Lane 实际节点、账号分配、证据不足、观察/自动模式、固定、手动测试与审计；危险操作二次确认并显示回滚 ID。
2. 运行：

   ```bash
   pnpm --dir frontend vitest run src/views/admin/__tests__/ProxyPoolView.spec.ts
   ```

   Expected: FAIL。
3. 实现页面，明确区分数据库期望状态与 Mihomo 实际状态；不返回账号 token、代理密码或 Mihomo secret。
4. 验证：

   ```bash
   pnpm --dir frontend vitest run src/views/admin/__tests__/ProxyPoolView.spec.ts
   pnpm --dir frontend run type-check
   ```

   Expected: PASS。
5. 提交：

   ```bash
   git add frontend/src/api/admin/proxyPool.ts frontend/src/views/admin/ProxyPoolView.vue frontend/src/views/admin/__tests__/ProxyPoolView.spec.ts frontend/src/router/index.ts frontend/src/types/index.ts frontend/src/i18n/locales
   git commit -m "feat(omni): add stable proxy pool admin view"
   ```

### Task 9: 增加单 Mihomo 部署配置并移除全局代理

**Files:**
- Create: `deploy/omni-v2/mihomo/config.yaml.tmpl`
- Create: `deploy/omni-v2/mihomo/README.md`
- Create: `deploy/omni-v2/scripts/render-mihomo-config.sh`
- Create: `deploy/omni-v2/scripts/verify-proxy-pool.sh`
- Modify: `deploy/omni-v2/docker-compose.yml`
- Modify: `deploy/omni-v2/.env.example`
- Create: `deploy/omni-v2/docker-compose_test.go`

**Steps:**

1. 写失败测试：只有一个 Mihomo；19081-19085 对应生产 Lane；3 个候选探测 Lane和 Canary 不暴露公网；controller 仅 Docker 内网可达并有 secret；订阅 URL 只来自服务器 secret；app 没有 `HTTP_PROXY/HTTPS_PROXY/ALL_PROXY`；容器有资源上限、健康检查和日志轮转；部署脚本不覆盖代理 ID `6`。
2. 运行：

   ```bash
   cd deploy/omni-v2 && go test ./... -run TestProxyPoolCompose -count=1
   ```

   Expected: FAIL。
3. 固定 Mihomo 镜像 digest，不用 `latest`。先用固定版本验证 `listeners[].proxy` 语法。模板只渲染到服务器运行目录，订阅 URL 与 controller secret 不进 Git。
4. 验证：

   ```bash
   cd deploy/omni-v2
   go test ./... -run TestProxyPoolCompose -count=1
   docker compose --env-file .env.test config >/tmp/sub2api-v2-compose.rendered.yml
   ```

   Expected: PASS，无公网代理端口。
5. 提交：

   ```bash
   git add deploy/omni-v2
   git commit -m "feat(omni): deploy five stable lanes on one mihomo"
   ```

### Task 10: 全量本地验收

**Files:**
- Create: `docs/governance/runs/43-v2-single-hop-stable-pool.md`
- Modify: `docs/plans/2026-07-22-43-v2-single-hop-stable-pool.md`

**Steps:**

1. 后端门禁：

   ```bash
   cd backend
   go test ./internal/config ./internal/repository ./internal/service ./internal/handler/admin ./internal/server/routes -count=1
   go test ./migrations -count=1
   go test ./... -count=1
   ```

2. 前端门禁：

   ```bash
   pnpm --dir frontend vitest run src/views/admin/__tests__/ProxyPoolView.spec.ts
   pnpm --dir frontend run type-check
   pnpm --dir frontend run lint:check
   ```

3. Run log 记录精确命令、输出、已知风险和生产前提，并明确尚未触碰 43。仓库若有基线失败，必须记录并证明与本改动无关。
4. 提交：

   ```bash
   git add docs/governance/runs/43-v2-single-hop-stable-pool.md docs/plans/2026-07-22-43-v2-single-hop-stable-pool.md
   git commit -m "docs(omni): record stable proxy pool acceptance"
   ```

### Task 11: 43 影子观察 2 小时

**Files:**
- Deploy from: `deploy/omni-v2/`
- Record remotely: `/data/sub2api-v2/ops/proxy-pool/`
- Update: `docs/governance/runs/43-v2-single-hop-stable-pool.md`

**Steps:**

1. 记录 V2 容器、内存、Swap、代理 ID `6`、OAuth 账号及其 `proxy_id`、真实请求结果；不打印 token、订阅 URL 或代理密码。
2. 只启动 Mihomo 和 `observe` 控制器，创建 Lane 代理但不绑定账号；确认现有请求仍通过账号代理 ID `6`。
3. 连续观察 2 小时，验收：29 个真实节点进入候选、3 个伪节点排除、约 20 分钟一轮、约 6 轮覆盖、Top 5 满足门槛、账号错误不污染节点评分、零账号时不自动切换。
4. Mihomo 内存异常、V2 OOM/重启、Legacy 健康变化、状态不一致或代理 ID `6` 请求失败时立即停止，不进入 Canary。

### Task 12: Canary、逐账号迁移与开启 auto

**Files:**
- Record remotely: `/data/sub2api-v2/ops/proxy-pool/canary/`
- Update: `docs/governance/runs/43-v2-single-hop-stable-pool.md`

**Steps:**

1. 从当前合格 OAuth 账号动态选择 Canary；只有一个就用该账号，没有账号则停止。
2. Canary Lane 观察 30 分钟，只承载受控验证，不接普通用户调度。
3. 账号逐个执行：记录旧 `proxy_id`、计算 Lane、更新、真实验证；失败立即恢复。迁移期间账号增删由下一轮 reconcile 处理。
4. 全部账号验证后才从 `observe` 切为 `auto`。
5. dry-run 验证所有受管账号回滚目标都是代理 ID `6`。多个 Lane 失败时逐账号恢复到 `6` 并关闭 `auto`。
6. 最终记录 Lane 实际节点、账号分配、2 小时观察、Canary、迁移、V2/Legacy 健康、资源和回滚证据；齐全后才标记完成。
