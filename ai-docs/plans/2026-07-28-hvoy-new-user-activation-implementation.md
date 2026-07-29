<!--
[INPUT]: 已确认的 HVOY 新用户激活方案、Sub2API 现有邮箱验证/订阅/计费/邮件/幂等能力，以及 43 V2 的 Omni 运行约束。
[OUTPUT]: 可按 TDD 执行的 HVOY 信任承接、新人体验、分层召回、邮件回访、指标验证与灰度发布计划。
[POS]: 位于 Omni 实施准备层，作为后续编码、测试、配置、发布和回滚的唯一执行清单。

[PROTOCOL]:
1. 一旦业务规则、状态机、文件范围、验证命令或发布门槛变化，必须同步更新本文。
2. 更新后必须检查 `ai-docs/plans/.folder.md` 的描述是否仍然准确。
-->

# HVOY 新用户激活实施计划

> **For Codex:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** 在 `dev/omni` 上实现 HVOY 信任承接、验证后 `$1/24h` 新手订阅、零成功用户分层、一次性 `$2/24h` 主动召回、人工支持和可复盘漏斗。

**Architecture:** 使用独立的 `user_activation_journeys` 记录激活状态，不把内部流程塞进用户属性。两个试用阶段分别使用独立订阅分组，额度上限由分组 `daily_limit_usd` 控制，时效由一天订阅控制；用户状态由使用记录、Key 和真实支付订单证据实时推导。注册同步尝试发放新手订阅，后台单实例扫描负责失败修复、状态收敛和邮件触达。

**Tech Stack:** Go、Gin、Ent、PostgreSQL、Wire、Vue 3、Pinia、TypeScript、Vitest、现有 Notification Email 与 Idempotency 能力。

---

## 0. 已锁定边界

### 0.1 业务常量

| 项目 | 固定规则 |
| --- | --- |
| 新手体验 | `$1`、24 小时、验证邮箱后发放 |
| 召回体验 | `$2`、24 小时、用户主动领取 |
| 召回窗口 | 注册后 7 天内 |
| 充值口径 | 最低 `¥10`，`¥1 = $1` API 计算额度 |
| 计费口径 | Pro 自营号池对用户按 `0.2x` 扣费 |
| 成功口径 | 至少一条 `usage_logs.actual_cost > 0` |
| 人工支持 | 微信 `welsir02`，不作为领额度条件 |

### 0.2 分层优先级

每次判断必须按以下顺序，前面的状态覆盖后面的状态：

```text
SUCCESS
  > PAID_ZERO_SUCCESS
  > ATTEMPTED_ZERO_SUCCESS
  > REGISTERED_NO_ATTEMPT
```

- `SUCCESS`：存在 `actual_cost > 0` 的使用记录。
- `PAID_ZERO_SUCCESS`：无成功使用，但存在 `order_type = balance`、`completed_at IS NOT NULL`、`pay_amount > 0` 的订单。
- `ATTEMPTED_ZERO_SUCCESS`：无成功、无已完成充值，但存在任意使用记录或至少一个有效 API Key。
- `REGISTERED_NO_ATTEMPT`：以上证据均不存在。

### 0.3 不允许改变的实现边界

- 不直接增加 `users.balance`，试用必须通过订阅完成。
- 不复用付费 Pro 分组，避免 `$1/$2` 上限污染付费用户。
- 新手和召回使用两个不同分组，均复用 Pro 账号池和 `0.2x` 计费。
- 发放调用 `SubscriptionService.AssignSubscription`，禁止调用会累加天数的 `AssignOrExtendSubscription`。
- 已充值但零成功的用户永远不进入 `$2` 领取分支。
- `USER_ACTIVATION_ENABLED` 默认关闭；迁移、代码、分组和邮件全部就绪后才能开启。
- 第一版只覆盖邮箱验证码注册；OAuth 注册不自动领取试用。
- `eligible_after` 之后所有完成验证码的邮箱注册都适用，新手资格不依赖 HVOY 来源；`campaign_source` 只用于归因，不能成为可伪造的授权条件。
- 不对启用时间之前的历史账号补发，资格下界由 `eligible_after` 控制。
- 不在本计划中同步、rebase 或部署其他分支。

## Task 1: 增加安全关闭的激活配置

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

**Step 1: 写失败的配置测试**

覆盖以下意图：

```go
func TestLoadUserActivationDefaultsDisabled(t *testing.T) {
    // enabled=false，且未配置分组时允许启动。
}

func TestLoadUserActivationRejectsInvalidEnabledConfig(t *testing.T) {
    // enabled=true 时，starter/recall group id 必须大于 0 且不能相同；
    // eligible_after 必须存在；支持微信不能为空。
}

func TestLoadUserActivationEnabledConfig(t *testing.T) {
    // 明确配置后，召回窗口固定为 7 天，扫描周期进入安全范围。
}
```

配置结构至少包含：

```go
type UserActivationConfig struct {
    Enabled             bool
    EligibleAfter       time.Time
    StarterGroupID      int64
    RecallGroupID       int64
    RecallWindowDays    int
    WorkerInterval      time.Duration
    SupportWeChat       string
}
```

环境变量：

```text
USER_ACTIVATION_ENABLED
USER_ACTIVATION_ELIGIBLE_AFTER
USER_ACTIVATION_STARTER_GROUP_ID
USER_ACTIVATION_RECALL_GROUP_ID
USER_ACTIVATION_RECALL_WINDOW_DAYS
USER_ACTIVATION_WORKER_INTERVAL_SECONDS
USER_ACTIVATION_SUPPORT_WECHAT
```

**Step 2: 验证 RED**

```bash
cd backend
go test ./internal/config -run TestLoadUserActivation -count=1
```

Expected: FAIL，`UserActivationConfig` 和校验尚不存在。

**Step 3: 最小实现**

- 默认 `enabled=false`、`recall_window_days=7`、`worker_interval=600s`。
- 开启时拒绝相同分组 ID、无资格起始时间、空支持微信或小于 60 秒的扫描周期。
- 不把 `$1/$2/0.2x/¥10` 做成可漂移配置；它们是本方案的业务合同，只有分组 ID 和运行开关属于部署配置。

**Step 4: 验证 GREEN**

```bash
cd backend
go test ./internal/config -run TestLoadUserActivation -count=1
```

Expected: PASS。

**Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat(config): define user activation rollout gates"
```

## Task 2: 建立可审计的激活旅程

**Files:**
- Create: `backend/ent/schema/user_activation_journey.go`
- Modify: `backend/ent/schema/user.go`
- Create: `backend/migrations/177_add_user_activation_journeys.sql`
- Create: `backend/migrations/user_activation_journey_migration_test.go`
- Generate: `backend/ent/*`

> 实施前先获取远端引用并重新检查最大迁移号。当前已知最大值是 `176`，但本工作树落后 `origin/dev/omni` 两个提交，若 `177` 已占用，必须改用下一个未占用号码，不能修改已有迁移。

**Step 1: 写失败的迁移测试**

测试 SQL 至少包含：

- `user_activation_journeys` 表；
- `user_id` 唯一约束和到 `users(id)` 的外键；
- 新手、召回订阅 ID 到 `user_subscriptions(id)` 的外键；
- 状态 CHECK；
- `campaign_source + created_at`、新手到期、召回状态索引；
- 不包含任何历史数据回填。

**Step 2: 验证 RED**

```bash
cd backend
go test ./migrations -run UserActivationJourney -count=1
```

Expected: FAIL，迁移文件不存在。

**Step 3: 定义最小持久化模型**

字段固定为：

```text
id
user_id                         UNIQUE NOT NULL
campaign_source                 NOT NULL DEFAULT 'direct'
starter_state                   pending|granted|expired
starter_subscription_id         NULL
starter_granted_at              NULL
starter_expires_at              NULL
recall_state                    locked|claimable|claimed|expired|blocked_paid|closed_success
recall_subscription_id          NULL
recall_claimed_at               NULL
recall_expires_at               NULL
first_success_at                NULL
no_attempt_email_sent_at        NULL
attempted_email_sent_at         NULL
paid_support_email_sent_at      NULL
recall_available_email_sent_at  NULL
recall_expired_email_sent_at    NULL
last_email_sent_at              NULL
last_evaluated_at               NULL
created_at
updated_at
```

状态字段用于可观测性；是否可领取仍必须在事务内根据最新证据重算，不能只信状态字段。

**Step 4: 生成 Ent**

```bash
cd backend
go generate ./ent
```

Expected: 新实体、查询、创建、更新、mutation 和 client 代码生成成功。

**Step 5: 验证 GREEN**

```bash
cd backend
go test ./migrations -run UserActivationJourney -count=1
go test ./ent/... -count=1
```

Expected: PASS。

**Step 6: Commit**

```bash
git add backend/ent backend/migrations/177_add_user_activation_journeys.sql backend/migrations/user_activation_journey_migration_test.go
git commit -m "feat(db): persist user activation journeys"
```

## Task 3: 实现旅程仓储和行为证据快照

**Files:**
- Create: `backend/internal/service/user_activation.go`
- Create: `backend/internal/repository/user_activation_repo.go`
- Create: `backend/internal/repository/user_activation_evidence_repo.go`
- Modify: `backend/internal/repository/wire.go`
- Test: `backend/internal/repository/user_activation_repo_test.go`
- Test: `backend/internal/repository/user_activation_evidence_repo_test.go`

**Step 1: 写失败的仓储测试**

服务端接口保持激活领域专用，不扩张通用 `UsageLogRepository`：

```go
type UserActivationJourneyRepository interface {
    CreateIfAbsent(ctx context.Context, userID int64, source string) (*UserActivationJourney, bool, error)
    GetByUserID(ctx context.Context, userID int64) (*UserActivationJourney, error)
    GetByUserIDForUpdate(ctx context.Context, userID int64) (*UserActivationJourney, error)
    Update(ctx context.Context, journey *UserActivationJourney) error
    MarkEmailSent(ctx context.Context, journeyID int64, stage UserActivationEmailStage, sentAt time.Time) error
    ListDue(ctx context.Context, now time.Time, afterID int64, limit int) ([]UserActivationJourney, error)
    ListEligibleUsersWithoutJourney(ctx context.Context, eligibleAfter time.Time, afterID int64, limit int) ([]User, error)
}

type UserActivationEvidenceRepository interface {
    Snapshot(ctx context.Context, userID int64) (*UserActivationEvidence, error)
}

type UserActivationEvidence struct {
    FirstSuccessfulUsageAt *time.Time
    FirstCompletedPaymentAt *time.Time
    LastAttemptAt          *time.Time
    FirstAPIKeyAt          *time.Time
    UsageCount             int64
    APIKeyCount            int64
}
```

测试必须证明：

- 并发 `CreateIfAbsent` 最终只有一行；
- 成功口径严格为 `actual_cost > 0`；
- 失败/零费用记录计入尝试，但不计入成功；
- 支付证据使用余额订单的 `completed_at` 和正 `pay_amount`，不使用 `users.total_recharged`；
- Key 数使用现有未删除 Key 口径；
- Key 首次出现时间使用未删除 Key 的 `MIN(created_at)`，用于证明 30 分钟等待窗口；
- 两个扫描列表都使用 `afterID` 稳定 keyset 游标，单用户失败不能饿死后续用户；
- `MarkEmailSent` 只更新对应阶段邮件字段和 `last_email_sent_at`，不能覆盖并发变化的 starter/recall 状态；
- SQL 参数化，不拼接用户输入。

**Step 2: 验证 RED**

```bash
cd backend
go test ./internal/repository -run 'UserActivation(Journey|Evidence)' -count=1
```

Expected: FAIL，新接口与实现不存在。

**Step 3: 最小实现**

- Journey CRUD 使用 Ent。
- 证据查询使用一个聚合 SQL 往返，读取 `usage_logs`、`api_keys`、`payment_orders`。
- `ListEligibleUsersWithoutJourney` 只返回：
  - `created_at >= eligible_after`；
  - `signup_source = 'email'`；
  - 未软删除；
  - 不存在 journey。
- 修复扫描创建的 journey 使用 `campaign_source='unknown'`；正常注册同步创建时保留 `hvoy_partner` 或 `direct`。

**Step 4: 验证 GREEN**

```bash
cd backend
go test ./internal/repository -run 'UserActivation(Journey|Evidence)' -count=1
```

Expected: PASS。

**Step 5: Commit**

```bash
git add backend/internal/service/user_activation.go backend/internal/repository/user_activation_repo.go backend/internal/repository/user_activation_evidence_repo.go backend/internal/repository/user_activation_repo_test.go backend/internal/repository/user_activation_evidence_repo_test.go backend/internal/repository/wire.go
git commit -m "feat(activation): add journey and evidence repositories"
```

## Task 4: 实现 `$1` 发放、状态推导和 `$2` 领取事务

**Files:**
- Create: `backend/internal/service/user_activation_service.go`
- Create: `backend/internal/service/user_activation_service_test.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/subscription_assign_idempotency_test.go`
- Modify: `backend/internal/service/wire.go`

**Step 1: 写服务 RED 测试**

至少覆盖：

1. 功能关闭时不创建 journey、不发订阅。
2. 资格时间之前的用户不发放。
3. 未开启邮箱验证时拒绝发放。
4. 合格注册只创建一份 `$1` 一天订阅。
5. 同一注册同步重试不会多发一天。
6. 成功使用后状态变为 `SUCCESS`，并写一次 `first_success_at`。
7. 充值零成功状态优先于 attempted/no-attempt。
8. 新手未过期不能领 `$2`。
9. 新手过期、零成功、零充值、七天内可以领 `$2`。
10. 已充值、已成功、超过七天或已领取时不能再次发放。
11. 两个 goroutine 同时领取，最终只有一个召回订阅。
12. `AssignSubscription` 失败时事务回滚，journey 仍可重试。
13. 外层事务提交前不异步回填旧订阅缓存，提交后同步失效 L1/Redis 缓存。

**Step 2: 验证 RED**

```bash
cd backend
go test ./internal/service -run UserActivation -count=1
```

Expected: FAIL，服务不存在。

**Step 3: 定义服务边界**

```go
type UserActivationBootstrapper interface {
    BootstrapVerifiedRegistration(
        ctx context.Context,
        user *User,
        campaignSource string,
    ) error
}

type UserActivationService struct {
    cfg             config.UserActivationConfig
    journeys        UserActivationJourneyRepository
    evidence        UserActivationEvidenceRepository
    subscriptions   *SubscriptionService
    settingService  *SettingService
    entClient       *ent.Client
}
```

公开方法：

```go
BootstrapVerifiedRegistration(ctx, user, source) error
GetStatus(ctx, userID) (*UserActivationStatus, error)
ClaimRecall(ctx, userID) (*UserActivationStatus, error)
Evaluate(ctx, userID, now) (*UserActivationStatus, error)
```

**Step 4: 实现状态和事务**

- `BootstrapVerifiedRegistration`：
  - 先验证 feature flag、资格时间和邮箱验证开关；
  - `CreateIfAbsent`；
  - 在 Ent 事务中锁定 journey；
  - 若新手尚未 granted，调用 `AssignSubscription`：

```go
&AssignSubscriptionInput{
    UserID:       user.ID,
    GroupID:      cfg.StarterGroupID,
    ValidityDays: 1,
    Notes:        "user_activation:starter",
}
```

  - 写入订阅 ID、grant/expiry 时间和 `starter_state=granted`。

- `Evaluate` 每次从证据快照重算分层；成功时间只首次写入。
- `ClaimRecall` 在同一事务内：
  - 先锁定 user，再锁定 journey，固定锁顺序；
  - 使用 transaction-aware SQL executor 重新读取成功和支付证据；
  - 检查新手到期、七天窗口和未领取；
  - 调用 `AssignSubscription`，Notes 为 `user_activation:recall`；
  - 写召回订阅 ID 和时间；
  - 返回同一个状态 DTO。
- 已经 claimed 的相同请求返回现有结果，不延长、不新建。
- 为现有 `assignSubscriptionWithReuse` 增加内部 `deferCacheInvalidation` 参数：
  - 公开 `AssignSubscription` 和 Bulk 路径仍传 `false`，行为不变；
  - activation 外层事务传 `true`；
  - journey 和 subscription 成功提交后调用现有 `invalidateSubscriptionCaches(userID, groupID)`；
  - 回滚时不失效缓存。

**Step 5: 验证 GREEN 和一天卡语义**

```bash
cd backend
go test ./internal/service -run 'UserActivation|OneTimeDailyQuota' -count=1
```

Expected: PASS，并继续证明一天订阅不会跨午夜重置 `$1/$2` 日上限。

**Step 6: Commit**

```bash
git add backend/internal/service/user_activation_service.go backend/internal/service/user_activation_service_test.go backend/internal/service/subscription_service.go backend/internal/service/subscription_assign_idempotency_test.go backend/internal/service/wire.go
git commit -m "feat(activation): grant starter and recall subscriptions"
```

## Task 5: 接入邮箱验证注册和 HVOY 来源

**Files:**
- Modify: `backend/internal/handler/auth_handler.go`
- Create: `backend/internal/handler/auth_handler_activation_test.go`
- Modify: `backend/internal/service/auth_service.go`
- Modify: `backend/internal/service/auth_service_register_test.go`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/auth/RegisterView.vue`
- Modify: `frontend/src/views/auth/EmailVerifyView.vue`
- Modify: `frontend/src/views/auth/__tests__/EmailVerifyView.spec.ts`
- Create: `frontend/src/views/auth/__tests__/RegisterActivationSource.spec.ts`

**Step 1: 写后端 RED 测试**

新增兼容方法，避免修改所有历史调用：

```go
type RegistrationContext struct {
    CampaignSource string
}

func (s *AuthService) RegisterWithVerificationContext(
    ctx context.Context,
    email, password, verifyCode, promoCode, invitationCode, affiliateCode string,
    registration RegistrationContext,
) (string, *User, error)
```

原 `RegisterWithVerification` 委托到新方法并传空 Context。

测试：

- `hvoy_partner` 被规范化并传给 bootstrapper；
- 未知来源降级为 `direct`，不能让注册失败；
- 没有验证码、邮箱后缀不允许、用户创建失败时不调用 bootstrapper；
- 激活 bootstrap 暂时失败时注册仍成功并显式记录错误，后续 worker 可修复；
- 旧 `RegisterWithVerification` 行为不变。

**Step 2: 验证后端 RED**

```bash
cd backend
go test ./internal/service -run 'Register.*Activation|Register.*Campaign' -count=1
go test ./internal/handler -run 'Register.*Campaign' -count=1
```

Expected: FAIL，新字段和依赖尚不存在。

**Step 3: 最小后端接入**

- `RegisterRequest` 增加 `campaign_source`。
- `AuthService` 构造函数注入 `UserActivationBootstrapper`。
- `UserActivationService` 不依赖 `AuthService`，因此 Wire 不形成依赖环。
- 仅在用户成功创建并通过邮箱验证码后调用 bootstrapper。
- bootstrap 失败不删除账号、不伪装成功发放；记录 `user_id/source/error`，等待 worker 修复。

**Step 4: 写前端 RED 测试**

证明：

- `/register?source=hvoy_partner&redirect=/activation` 只接受允许的来源和站内路径；
- `campaign_source` 与 redirect 在跳转 `/email-verify` 时写入 `register_data`；
- EmailVerify 提交时带上 `campaign_source`；
- 成功后跳转 `/activation`；
- 恶意外链 redirect 和未知 source 被丢弃。

**Step 5: 验证前端 RED**

```bash
pnpm --dir frontend exec vitest run \
  src/views/auth/__tests__/RegisterActivationSource.spec.ts \
  src/views/auth/__tests__/EmailVerifyView.spec.ts
```

Expected: FAIL，注册数据尚未保留激活来源。

**Step 6: 实现并验证 GREEN**

```bash
cd backend
go test ./internal/service -run 'Register.*Activation|Register.*Campaign' -count=1
go test ./internal/handler -run 'Register.*Campaign' -count=1
cd ..
pnpm --dir frontend exec vitest run \
  src/views/auth/__tests__/RegisterActivationSource.spec.ts \
  src/views/auth/__tests__/EmailVerifyView.spec.ts
```

Expected: PASS。

**Step 7: Commit**

```bash
git add backend/internal/handler/auth_handler.go backend/internal/handler/auth_handler_activation_test.go backend/internal/service/auth_service.go backend/internal/service/auth_service_register_test.go frontend/src/types/index.ts frontend/src/views/auth/RegisterView.vue frontend/src/views/auth/EmailVerifyView.vue frontend/src/views/auth/__tests__/EmailVerifyView.spec.ts frontend/src/views/auth/__tests__/RegisterActivationSource.spec.ts
git commit -m "feat(auth): bootstrap verified user activation"
```

## Task 6: 提供公开 Offer、用户状态和幂等领取 API

**Files:**
- Create: `backend/internal/handler/activation_handler.go`
- Create: `backend/internal/handler/activation_handler_test.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/common.go`
- Modify: `backend/internal/server/routes/user.go`
- Create: `backend/internal/server/routes/activation_routes_test.go`
- Modify: `backend/cmd/server/wire.go`
- Generate: `backend/cmd/server/wire_gen.go`

**Step 1: 写 handler/route RED 测试**

接口：

```text
GET  /api/v1/public/activation/hvoy
GET  /api/v1/user/activation
POST /api/v1/user/activation/recall/claim
```

公开 Offer 固定返回：

```json
{
  "enabled": true,
  "starter_credit_usd": 1,
  "starter_valid_hours": 24,
  "recall_credit_usd": 2,
  "recall_valid_hours": 24,
  "recall_window_days": 7,
  "minimum_recharge_cny": 10,
  "recharge_credit_rate": 1,
  "pro_rate_multiplier": 0.2,
  "support_wechat": "welsir02"
}
```

用户状态至少返回：

```go
type UserActivationStatus struct {
    Enabled        bool
    Segment        string
    FirstSuccessAt *time.Time
    Starter        ActivationGrantStatus
    Recall         ActivationRecallStatus
    ActiveGroup    *ActivationGroupStatus
    SupportWeChat  string
    NextAction     string
}
```

测试必须证明：

- 公共接口不开启时只返回 `enabled=false`，不承诺赠送；
- 用户状态接口必须鉴权，且只能看自己；
- recall claim 必须带 `Idempotency-Key`；
- 相同 key 重放返回 `X-Idempotency-Replayed: true`；
- 不同 key 并发也不能发两份订阅；
- paid/no-success 返回业务冲突且不发订阅。

**Step 2: 验证 RED**

```bash
cd backend
go test ./internal/handler ./internal/server/routes -run Activation -count=1
```

Expected: FAIL，handler 和路由不存在。

**Step 3: 最小实现**

- claim 使用现有 `executeUserIdempotentJSON`，scope 为 `user.activation.recall.claim`。
- `Handlers`、Wire provider 和用户/公共路由只增加本功能所需依赖。
- 公开接口不返回内部 group ID；鉴权状态接口可返回当前试用 group ID/platform，供 Key 创建。
- 错误使用现有 `infraerrors` 和统一 response，不增加第二套错误格式。

**Step 4: 生成 Wire 并验证 GREEN**

```bash
cd backend
go generate ./cmd/server
go test ./internal/handler ./internal/server/routes -run Activation -count=1
go test ./cmd/server -count=1
```

Expected: PASS，Wire 无缺失或循环依赖。

**Step 5: Commit**

```bash
git add backend/internal/handler/activation_handler.go backend/internal/handler/activation_handler_test.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/internal/server/routes/common.go backend/internal/server/routes/user.go backend/internal/server/routes/activation_routes_test.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(api): expose activation status and recall claim"
```

## Task 7: 增加分层邮件和可恢复扫描任务

**Files:**
- Modify: `backend/internal/service/notification_email_service.go`
- Modify: `backend/internal/service/notification_email_service_test.go`
- Create: `backend/internal/service/user_activation_worker.go`
- Create: `backend/internal/service/user_activation_worker_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire.go`
- Generate: `backend/cmd/server/wire_gen.go`
- Modify: `backend/cmd/server/wire_gen_test.go`
- Modify: `backend/internal/service/user_activation.go`
- Modify: `backend/internal/repository/user_activation_evidence_repo.go`
- Modify: `backend/internal/repository/user_activation_evidence_repo_test.go`
- Modify: `backend/internal/repository/user_activation_repo.go`
- Modify: `backend/internal/repository/user_activation_repo_test.go`
- Modify: `ai-docs/plans/2026-07-28-hvoy-new-user-activation-implementation.md`

**Step 1: 写邮件事件 RED 测试**

新增五个 optional 事件：

```text
activation.no_attempt
activation.attempted_zero_success
activation.paid_zero_success
activation.recall_available
activation.recall_expired
```

要求：

- zh/en 官方模板都存在；
- 所有事件 `Optional=true` 并包含退订链接；
- 模板变量只使用声明过的 placeholder；
- `SourceType=user_activation_journey`、`SourceID=journey.ID`、`ReminderKey=stage` 可去重。

**Step 2: 写 worker RED 测试**

单轮优先级：

```text
paid_zero_success
  > recall_expired
  > recall_available
  > attempted_zero_success
  > no_attempt
```

时间规则：

- Key/失败请求出现后至少 30 分钟仍零成功：attempted 邮件；
- Key 年龄由 `FirstAPIKeyAt` 证明；同时存在 Key 和失败请求时，从两者较晚时间开始计算 30 分钟；
- 注册满 2 小时且没有 Key/调用/支付：no-attempt 邮件；
- 完成充值后下一轮扫描：paid-support 邮件；
- 新手订阅到期、零成功、零充值且仍在 7 天窗口：recall-available 邮件；
- `$2` 到期仍零成功：recall-expired 最终支持邮件；
- 除 paid-support 外，任意两封激活邮件至少间隔 12 小时；
- paid-support 发现已完成充值时绕过冷却，下一轮立即发送；
- 一轮最多发送一个阶段；
- 每次发送前再次读取成功证据；
- 成功用户不再发送任何激活邮件；
- paid/no-success 邮件不得包含领取链接。

还要证明：

- 多实例只允许 leader 扫描；
- 注册同步发放失败的合格账号会被补建 journey 并重试 `$1`；
- 单用户异常不终止整批扫描；
- `Stop()` 能等待 worker 正常退出。

**Step 3: 验证 RED**

```bash
cd backend
go test ./internal/service -run 'NotificationEmail.*Activation|UserActivationWorker' -count=1
```

Expected: FAIL，新事件和 worker 不存在。

**Step 4: 实现 worker**

- 复用 `SubscriptionExpiryService` 的 leader lock/DB advisory lock 模式。
- lock key 使用 `user_activation:worker:leader`，TTL 必须大于一轮最大超时。
- 每轮先分页修复 `eligible_after` 之后无 journey 的邮箱注册用户，再分页评估 due journey。
- 两类分页均使用 `afterID` 稳定游标；due 列表使用本轮固定 cutoff，避免 `Evaluate` 写入新时间后在同一轮再次入选。
- 已有 journey 但 `StarterSubscriptionID=nil` 时也调用同一 bootstrap 路径重试 `$1`。
- 发送成功或被退订/去重后写相应 `*_email_sent_at` 和 `last_email_sent_at`。
- 邮件写回使用仓储窄更新，只修改对应阶段邮件字段和 `last_email_sent_at`。
- 失败只记录阶段、journey ID、user ID 和错误，不记录完整邮箱或邮件正文。

**Step 5: Wire、cleanup 和 GREEN**

将 worker 加入 provider set 并在 `provideCleanup` 中 Stop：

```bash
cd backend
go generate ./cmd/server
go test ./internal/service -run 'NotificationEmail.*Activation|UserActivationWorker' -count=1
go test ./cmd/server -count=1
```

Expected: PASS。

**Step 6: Commit**

```bash
git add backend/internal/service/notification_email_service.go backend/internal/service/notification_email_service_test.go backend/internal/service/user_activation_worker.go backend/internal/service/user_activation_worker_test.go backend/internal/service/wire.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(activation): send segmented recovery emails"
```

## Task 8: 构建 HVOY 信任承接页

**Files:**
- Create: `frontend/src/views/public/HvoyPartnerView.vue`
- Create: `frontend/src/views/public/__tests__/HvoyPartnerView.spec.ts`
- Create: `frontend/src/api/activation.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/i18n/locales/zh/landing.ts`
- Modify: `frontend/src/i18n/locales/en/landing.ts`

**Step 1: 写 RED 测试**

页面路由为：

```text
/partner/hvoy
```

测试：

- 页面读取公共 Offer，不自行硬编码仍可领取的承诺；
- `enabled=false` 时隐藏“注册送 `$1`”CTA，改为登录/查看服务；
- enabled 时主 CTA 为 `/register?source=hvoy_partner&redirect=/activation`；
- 次 CTA 为 `/login?redirect=/activation`；
- 首屏明确显示 `$1/24h`、`¥1=$1`、`0.2x`、付费额度不过期和微信支持；
- 不出现“加微信领钱”；
- mobile/desktop 文字不溢出，下一段内容在首屏底部可见。

**Step 2: 验证 RED**

```bash
pnpm --dir frontend exec vitest run src/views/public/__tests__/HvoyPartnerView.spec.ts
```

Expected: FAIL，页面和 API 不存在。

**Step 3: 最小实现**

- 这是信任承接页，不是第二个营销首页。
- 使用现有全局 token、按钮、图标和响应式布局。
- 不创建嵌套卡片；用清晰的全宽信息带表达价格、时效、支持和流程。
- 支持链接只复制/展示 `welsir02`，不与额度绑定。
- HVOY 后台最终只填写这个 URL，不直跳充值或通用 Dashboard。

**Step 4: GREEN**

```bash
pnpm --dir frontend exec vitest run src/views/public/__tests__/HvoyPartnerView.spec.ts
pnpm --dir frontend run typecheck
```

Expected: PASS。

**Step 5: Commit**

```bash
git add frontend/src/views/public/HvoyPartnerView.vue frontend/src/views/public/__tests__/HvoyPartnerView.spec.ts frontend/src/api/activation.ts frontend/src/router/index.ts frontend/src/i18n/locales/zh/landing.ts frontend/src/i18n/locales/en/landing.ts
git commit -m "feat(web): add Hvoy trust handoff page"
```

## Task 9: 构建验证后的首次成功工作台

**Files:**
- Create: `frontend/src/views/user/ActivationView.vue`
- Create: `frontend/src/views/user/__tests__/ActivationView.spec.ts`
- Create: `frontend/src/stores/activation.ts`
- Create: `frontend/src/stores/__tests__/activation.spec.ts`
- Modify: `frontend/src/api/activation.ts`
- Modify: `frontend/src/api/keys.ts`
- Create: `frontend/src/api/__tests__/keysActivation.spec.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/i18n/locales/zh/dashboard.ts`
- Modify: `frontend/src/i18n/locales/en/dashboard.ts`
- Reuse: `frontend/src/components/keys/UseKeyModal.vue`

**Step 1: 写 store/API RED 测试**

测试：

- status 加载和刷新；
- recall claim 自动生成并复用同一个 `Idempotency-Key`，网络重试不换 key；
- replay 返回相同状态；
- claim 成功后刷新 active group；
- paid/no-success 和 success 状态不暴露 claim action。
- 激活页创建 Key 时发送非空 `Idempotency-Key`，同一次自动重试复用该值。

**Step 2: 写页面 RED 测试**

覆盖四个分层和两个订阅阶段：

- 新手 active：显示剩余额度/到期时间和“创建体验 Key”；
- 已有 Key：允许直接打开现有 `UseKeyModal`；
- attempted/no-success：显示 Base URL、模型、Key、请求格式排查，不再只提示充值；
- paid/no-success：微信支持是主操作，隐藏 `$2`；
- recall claimable：主动确认后领取，不自动请求；
- success：停止激活提示，主操作进入 `/purchase` 充值；
- recall expired/no-success：只保留排查和人工访谈，不再补贴。

**Step 3: 验证 RED**

```bash
pnpm --dir frontend exec vitest run \
  src/api/__tests__/keysActivation.spec.ts \
  src/stores/__tests__/activation.spec.ts \
  src/views/user/__tests__/ActivationView.spec.ts
```

Expected: FAIL，store 和页面不存在。

**Step 4: 实现最短首次调用路径**

- 新增鉴权路由 `/activation`。
- 扩展现有 `keysAPI.create`，允许调用方传 `idempotencyKey` 并设置请求头；不改变 KeysView 的其他参数语义。
- 创建 Key 时直接调用 `keysAPI.create`，固定名称可为 `HVOY Trial`，group ID 使用 status 返回的 active trial group。
- 创建成功后立即用真实 Key、公共 Base URL、group platform 打开现有 `UseKeyModal`。
- 不复制 `KeysView` 的完整高级编辑表单。
- 页面提供“刷新调用状态”，不伪造测试成功。
- 成功后指向默认就是充值 Tab 的 `/purchase`。
- 微信始终描述为配置支持，而不是奖励领取条件。

**Step 5: GREEN**

```bash
pnpm --dir frontend exec vitest run \
  src/api/__tests__/keysActivation.spec.ts \
  src/stores/__tests__/activation.spec.ts \
  src/views/user/__tests__/ActivationView.spec.ts \
  src/components/keys/__tests__/UseKeyModal.spec.ts
pnpm --dir frontend run typecheck
```

Expected: PASS。

**Step 6: Commit**

```bash
git add frontend/src/views/user/ActivationView.vue frontend/src/views/user/__tests__/ActivationView.spec.ts frontend/src/stores/activation.ts frontend/src/stores/__tests__/activation.spec.ts frontend/src/api/activation.ts frontend/src/api/keys.ts frontend/src/api/__tests__/keysActivation.spec.ts frontend/src/types/index.ts frontend/src/router/index.ts frontend/src/i18n/locales/zh/dashboard.ts frontend/src/i18n/locales/en/dashboard.ts
git commit -m "feat(web): guide activated users to first success"
```

## Task 10: 完成全链路测试和指标基线

**Files:**
- Create: `backend/internal/integration/user_activation_flow_test.go`
- Modify: `ai-docs/plans/2026-07-28-hvoy-new-user-activation-design.md` only if implementation reveals a real contract change
- Update relevant folder documentation required by the files actually changed

**Step 1: 写集成测试**

用真实 PostgreSQL 测试以下链路：

```text
HVOY 来源
-> 邮箱验证码注册
-> journey + $1 一天订阅
-> 创建 Key
-> 记录 actual_cost=0 的失败尝试
-> 状态 attempted_zero_success
-> 新手到期
-> 幂等领取 $2
-> 记录 actual_cost>0
-> 状态 success
-> 后续 worker 不再发激活邮件
```

另写独立链路：

```text
注册 -> 完成余额订单 -> 零成功
-> paid_zero_success
-> claim 被拒绝
-> 只产生 paid support 邮件
```

**Step 2: 运行后端验证**

```bash
cd backend
gofmt -w \
  ent/schema/user_activation_journey.go \
  ent/schema/user.go \
  internal/config/config.go \
  internal/config/config_test.go \
  internal/repository/user_activation_repo.go \
  internal/repository/user_activation_repo_test.go \
  internal/repository/user_activation_evidence_repo.go \
  internal/repository/user_activation_evidence_repo_test.go \
  internal/repository/wire.go \
  internal/service/user_activation.go \
  internal/service/user_activation_service.go \
  internal/service/user_activation_service_test.go \
  internal/service/user_activation_worker.go \
  internal/service/user_activation_worker_test.go \
  internal/service/subscription_service.go \
  internal/service/subscription_assign_idempotency_test.go \
  internal/service/notification_email_service.go \
  internal/service/notification_email_service_test.go \
  internal/service/auth_service.go \
  internal/service/auth_service_register_test.go \
  internal/service/wire.go \
  internal/handler/auth_handler.go \
  internal/handler/auth_handler_activation_test.go \
  internal/handler/activation_handler.go \
  internal/handler/activation_handler_test.go \
  internal/handler/handler.go \
  internal/handler/wire.go \
  internal/server/routes/common.go \
  internal/server/routes/user.go \
  internal/server/routes/activation_routes_test.go \
  cmd/server/wire.go \
  internal/integration/user_activation_flow_test.go
go test ./migrations -run UserActivation -count=1
go test ./internal/repository -run UserActivation -count=1
go test ./internal/service -run 'UserActivation|NotificationEmail.*Activation|Register.*Activation' -count=1
go test ./internal/handler ./internal/server/routes -run Activation -count=1
go test -tags=integration ./internal/integration -run UserActivationFlow -count=1
go test ./...
go build ./cmd/server
```

Expected: 全部 PASS；若 integration 环境不可用，必须明确报告，不能以 unit test 替代并声称全链路完成。

**Step 3: 运行前端验证**

```bash
pnpm --dir frontend exec vitest run \
  src/views/public/__tests__/HvoyPartnerView.spec.ts \
  src/views/auth/__tests__/RegisterActivationSource.spec.ts \
  src/views/auth/__tests__/EmailVerifyView.spec.ts \
  src/api/__tests__/keysActivation.spec.ts \
  src/stores/__tests__/activation.spec.ts \
  src/views/user/__tests__/ActivationView.spec.ts \
  src/components/keys/__tests__/UseKeyModal.spec.ts
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend run build
```

Expected: 全部 PASS。

**Step 4: 浏览器验收**

在桌面 `1440x900` 和移动 `390x844` 验收：

- `/partner/hvoy` 首屏可见真实 Offer 和主操作；
- 注册、验证码页和 `/activation` 无跳转断层；
- Key 创建后配置 Modal 可用；
- recall/paid/success 状态不会出现互相冲突的 CTA；
- 长邮箱、长错误文本、中英文不溢出或遮挡；
- 网络面板没有重复 claim 或无限轮询。

**Step 5: 建立上线前零样本基线**

代码上线前保存以下 cohort 数据，时间统一使用 UTC：

- HVOY 后台有效点击；
- `/partner/hvoy` Nginx 去重访问；
- `campaign_source=hvoy_partner` 注册数；
- 邮箱验证完成注册数；
- 24 小时内首次成功数；
- 首次充值数；
- 已充值但零成功数。

点击数据来自 HVOY，站内漏斗来自 journey/usage/payment；不能把两个不同来源的样本强行当成同一张精确用户表。

**Step 6: Commit**

```bash
git add backend/internal/integration/user_activation_flow_test.go
git commit -m "test(activation): verify Hvoy conversion journey"
```

## Task 11: 配置两个独立试用分组

此任务只在代码已经通过 Task 10、且进入经用户授权的 43 V2 发布窗口后执行。

### 11.1 分组合同

| 属性 | Starter group | Recall group |
| --- | --- | --- |
| 用途 | 新注册验证价值 | 沉默用户主动回来再试 |
| subscription type | subscription | subscription |
| validity | 由发放设置 1 天 | 由发放设置 1 天 |
| daily_limit_usd | `1.0` | `2.0` |
| weekly/monthly limit | 不设置 | 不设置 |
| rate_multiplier | `0.2` | `0.2` |
| platform | 与付费 Pro 一致 | 与付费 Pro 一致 |
| account pool | 复制付费 Pro 当前关联 | 复制付费 Pro 当前关联 |
| public purchase | 关闭 | 关闭 |

### 11.2 配置验证

- 读取当前付费 Pro group ID、账号关联、模型可用范围和倍率。
- 新建两个 group，不更新付费 Pro 的日上限。
- 比较三个组的账号 ID 集合和模型集合；必须一致后才能写入环境变量。
- 用测试用户分别发放一天卡，验证：
  - `$1/$2` 上限；
  - 24 小时 expiry；
  - 跨午夜不重置；
  - 余额充值仍永久有效；
  - 付费 Pro 用户不受新组影响。
- 记录两个新 group ID，禁止把名字或临时环境 ID硬编码进源码。

## Task 12: 43 V2 灰度、指标和回滚

### 12.1 发布顺序

1. 再次确认目标是 43 V2，不是 V1 preview。
2. 备份当前 V2 镜像、环境变量和数据库 schema 版本。
3. 先应用向前兼容迁移。
4. 发布代码，但保持 `USER_ACTIVATION_ENABLED=false`。
5. 验证健康检查、现有注册、Key、支付、订阅和邮件没有回归。
6. 配置两个试用 group ID、`eligible_after`、微信和扫描周期。
7. 在后台确认强制邮箱验证已开启，邮箱后缀白名单已配置。
8. 用内部测试邮箱走完整链路。
9. 将 HVOY 跳转地址改为 `/partner/hvoy`。
10. 开启 feature flag，先观察小样本，再扩大。

### 12.2 邮箱白名单起始配置

首批允许：

```text
gmail.com
qq.com
foxmail.com
163.com
126.com
outlook.com
hotmail.com
icloud.com
```

企业邮箱的 MX 自动判定不放进第一版代码。先收集被拒绝但真实的企业用户，再决定是否增加经过人工审核的域名，避免把防刷方案重新变成任意邮箱可注册。

### 12.3 每日漏斗

按 `campaign_source` 和注册日期 cohort 计算：

```text
landing visit
-> verified registration
-> starter granted
-> key/attempt
-> first success within 24h
-> completed first recharge
-> active usage on day 7
```

召回单独计算：

```text
starter expired with zero success
-> recall email
-> recall claimed
-> first success after recall
-> first recharge after recall
```

成本计算：

```text
赠送成本 = starter subscription actual_cost + recall subscription actual_cost
获客赠送成本 = 赠送成本 / 新增首充用户数
```

不能用 `$1 + $2` 面值直接当现金成本，也不能把 `0.2x` 用户扣费和上游边际成本混为一谈。

### 12.4 首轮判断窗口

- 前 24 小时只判断注册、验证、Starter 发放和系统错误。
- 满 3 天判断验证后首次成功率、paid/no-success 和支持量。
- 满 7 天判断召回领取与召回成功。
- 满 14 天判断首次充值和 7 日继续使用。

在每个 cohort 少于 30 个验证注册前，只报告比例和绝对人数，不宣称统计显著。

### 12.5 止损门槛

任一条件满足即关闭 `USER_ACTIVATION_ENABLED`，保留已发订阅自然到期：

- Starter 发放失败率连续 15 分钟超过 2%；
- 同一用户出现多份 Starter 或 Recall 订阅；
- paid/no-success 用户成功领取 `$2`；
- 邮件重复发送率超过 1%；
- 激活 worker 持续失败或 leader lock 失效；
- 新功能导致注册、支付、计费或订阅主链路回归。

以下只触发策略复盘，不自动技术回滚：

- 召回领取高但成功率接近零；
- 赠送用量上升但首充没有改善；
- 人工支持量超过当前承接能力；
- 邮箱白名单误伤率持续上升。

### 12.6 回滚

1. 关闭 feature flag，停止新 journey、新发放和激活邮件。
2. HVOY 跳转回稳定页面或保留 trust page 但隐藏赠送 CTA。
3. 已发的一天订阅不删除，让其自然到期。
4. 不删除 journey 和邮件记录，保留审计与复盘数据。
5. 代码回滚到旧镜像；数据库迁移保留，因为是新增表且旧代码不会读取。
6. 只有确认新表导致独立数据库问题时，才在单独审批下做破坏性 schema 回滚。

## 最终验收

实现完成必须同时满足：

- 只有验证邮箱后的合格新注册能获得一次 `$1/24h`；
- `$2/24h` 只能在新手过期、零成功、零充值、七天内主动领取一次；
- paid/no-success 永远只进入支持分支；
- 成功使用后停止所有激活邮件；
- 两个试用分组不改变付费 Pro 行为；
- 注册、发放、claim、邮件均可重试且不重复产生价值；
- HVOY 点击后的页面、注册、验证和首次调用没有跳转断层；
- 能按 cohort 回答“卡在哪一步”，而不是只看注册数；
- `dev/omni` 以外的任何分支、V1 和其他服务器均未被修改。
