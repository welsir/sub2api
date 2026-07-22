# 43 V2 Single-Hop Stable Pool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 只为 43 V2 部署独立单跳稳定代理池；不修改 Sub2API 核心代码，不改变 V1/Legacy 的网络、容器、账号或入口。

**Architecture:** `deploy/omni-v2/proxy-pool/` 包含单 Mihomo 和独立 Go 控制器。Mihomo 提供 5 条生产 Lane、3 条候选探测 Lane和 1 条 Canary Lane；控制器读取 Mihomo API、维护文件状态，并只通过 V2 管理 API 管理 V2 账号的 `proxy_id`。

**Tech Stack:** Go, Docker Compose overlay, Mihomo REST API, Sub2API V2 Admin API, JSON state, JSONL audit

---

## 硬边界

- 不修改 `backend/`、`frontend/`、Ent schema 或数据库迁移。
- 不操作 `sub2api-preview-*`、V1 Nginx、V1 PostgreSQL、V1 Redis 或 V1账号。
- 远程目标只允许 `sub2api-v2-*`、`/data/sub2api-v2/proxy-pool/` 和 V2 管理 API。
- V2 当前代理 ID `6` 保留为回滚目标。
- 应用容器的全局代理环境变量只能在 V2 Compose 中处理。
- 工作树已有 V2 部署与计费改动不纳入本任务提交。

### Task 1: 独立控制器契约

**Files:**
- Create: `deploy/omni-v2/proxy-pool/go.mod`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/types.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/config.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/config_test.go`

1. RED：测试 `observe`、5/3/1 Lane、2 分钟、99%、3 连胜、P95 2.5 秒、5 秒退化、2 次失败、3 个慢窗口、20%、30 分钟冷却、2 小时观察、30 分钟 Canary、回滚 ID `6`。
2. RED：测试 Sub2API Base URL 和实例标识必须明确是 V2，出现 preview/V1 时拒绝启动。
3. Run：

   ```bash
   cd deploy/omni-v2/proxy-pool
   go test ./internal/pool -run 'TestConfig|TestTypes|TestV2Guard' -count=1
   ```

4. GREEN：实现配置与领域类型，重跑至 PASS。

### Task 2: Mihomo 客户端与节点发现

**Files:**
- Create: `deploy/omni-v2/proxy-pool/internal/mihomo/client.go`
- Create: `deploy/omni-v2/proxy-pool/internal/mihomo/client_test.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/discovery.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/discovery_test.go`

1. RED：用 `httptest.Server` 测试 provider 读取、伪节点过滤、实际选择器读取、切换后回读与不确定状态。
2. Run：

   ```bash
   cd deploy/omni-v2/proxy-pool
   go test ./internal/mihomo ./internal/pool -run 'TestMihomo|TestDiscovery' -count=1
   ```

3. GREEN：实现短超时、有限响应、secret 脱敏和 read-after-write；控制 API 不可达时不盲切。

### Task 3: 网络探测、评分与文件状态

**Files:**
- Create: `deploy/omni-v2/proxy-pool/internal/pool/probe.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/probe_test.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/score.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/score_test.go`
- Create: `deploy/omni-v2/proxy-pool/internal/state/store.go`
- Create: `deploy/omni-v2/proxy-pool/internal/state/store_test.go`

1. RED：匿名 401 属于传输可达；timeout/TLS/EOF/502/503 属于失败。
2. RED：99%/3 连胜/P95 门槛；性能替换 20%/3 窗口/30 分钟；明确故障绕过冷却。
3. RED：状态原子写入和 JSONL 审计。
4. Run：

   ```bash
   cd deploy/omni-v2/proxy-pool
   go test ./internal/pool ./internal/state -run 'TestProbe|TestScore|TestStore' -count=1
   ```

5. GREEN：最小实现并重跑至 PASS。

### Task 4: 只面向 V2 的 Sub2API 管理客户端

**Files:**
- Create: `deploy/omni-v2/proxy-pool/internal/sub2api/client.go`
- Create: `deploy/omni-v2/proxy-pool/internal/sub2api/client_test.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/affinity.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/affinity_test.go`

1. RED：登录、列出有效 OpenAI OAuth、更新 `proxy_id`、账号 SSE 测试。
2. RED：单账号、零账号、一致性亲和、逐账号验证与失败回滚。
3. RED：Base URL 或实例标识不是 V2 时不发送任何写请求。
4. Run：

   ```bash
   cd deploy/omni-v2/proxy-pool
   go test ./internal/sub2api ./internal/pool -run 'TestSub2API|TestAffinity|TestV2Guard' -count=1
   ```

5. GREEN：最小实现并重跑至 PASS。

### Task 5: 控制循环、Canary 与内部管理 API

**Files:**
- Create: `deploy/omni-v2/proxy-pool/internal/pool/controller.go`
- Create: `deploy/omni-v2/proxy-pool/internal/pool/controller_test.go`
- Create: `deploy/omni-v2/proxy-pool/cmd/controller/main.go`

测试每 2 分钟检查生产 Lane和 3 个候选、不叠加轮次、`observe` 不写账号、`auto` 才执行、Canary 记录旧代理并失败回滚、全局保护回滚 ID `6`、固定 Lane 在故障时仍撤离。

内部 API 只监听容器内网，提供 status、nodes、lanes、assignments、mode、test、pin、audits。

### Task 6: Mihomo 配置与 Compose overlay

**Files:**
- Create: `deploy/omni-v2/proxy-pool/mihomo/config.yaml.tmpl`
- Create: `deploy/omni-v2/proxy-pool/docker-compose.proxy-pool.yml`
- Create: `deploy/omni-v2/proxy-pool/Dockerfile`
- Create: `deploy/omni-v2/proxy-pool/scripts/render-config.sh`
- Create: `deploy/omni-v2/proxy-pool/scripts/verify-v2-boundary.sh`
- Create: `deploy/omni-v2/proxy-pool/compose_test.go`

测试确认：一个 Mihomo、5 个生产 Lane、3 个 Probe、1 个 Canary、无公网端口、controller secret、订阅 URL 不入 Git、资源上限、日志轮转、V2 app 无全局代理变量、操作脚本不出现 V1 服务名和路径。

### Task 7: 本地全量验收

```bash
cd deploy/omni-v2/proxy-pool
go test ./... -count=1
go vet ./...
docker compose -f ../docker-compose.yml -f docker-compose.proxy-pool.yml config
./scripts/verify-v2-boundary.sh
```

记录 `docs/governance/runs/43-v2-single-hop-stable-pool.md`，明确没有触碰 43。

### Task 8: 43 V2 影子部署与 2 小时观察

1. 通过 `tml-ssh-ops` 记录 V1 与 V2 容器、重启次数、健康和 V2 代理 ID `6`。
2. 只创建 `/data/sub2api-v2/proxy-pool/`，只启动 V2 Mihomo 和控制器 `observe`。
3. 不绑定账号，观察 2 小时；29 个真实节点进入候选、3 个伪节点排除、约 6 轮覆盖。
4. 任一 V1 容器重启次数变化或健康异常，立即停止 V2 新组件并调查，不对 V1 执行修复性改动。

### Task 9: V2 Canary、迁移与 auto

1. 动态选择一个 V2 OAuth 账号；只有一个就用它，没有账号则停止。
2. 记录旧 `proxy_id`，迁到 Canary 代理，验证 30 分钟；失败立即恢复。
3. V2 账号逐个迁移到 Lane，每次更新后 SSE 验证，失败恢复。
4. 全部验证后开启 `auto`。
5. 演练所有受管 V2 账号回滚到代理 ID `6`；不读取或写入 V1 账号。
