# XunhuPay Provider Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a production-safe native XunhuPay provider to Omni V2 for WeChat QR-code balance recharges, with signed callbacks, order recovery, and explicit admin routing.

**Architecture:** Implement `xunhupay` as a first-class adapter behind the existing `payment.Provider` interface. Keep protocol signing and HTTP handling in the provider package, keep amount/order/idempotency decisions in the existing payment service, and extend the existing visible-method source mapping so user-facing `wxpay` routes to the selected XunhuPay instance.

**Tech Stack:** Go 1.x, Gin, Ent, testify/httptest, Vue 3, TypeScript, Vitest, pnpm.

---

### Task 1: Register the provider contract and lock refund behavior

**Files:**
- Modify: `backend/internal/payment/types.go`
- Modify: `backend/internal/payment/provider/factory.go`
- Modify: `backend/internal/service/payment_config_providers.go`
- Test: `backend/internal/service/payment_config_providers_test.go`
- Test: `backend/internal/payment/provider/xunhupay_test.go`

**Step 1: Write the failing tests**

Add tests proving:

```go
func TestNewXunhuPayRequiresCredentials(t *testing.T) {
    _, err := NewXunhuPay("instance-1", map[string]string{})
    require.ErrorContains(t, err, "appId")
}

func TestCreateXunhuPayProviderForcesRefundDisabled(t *testing.T) {
    // Create an enabled xunhupay instance with RefundEnabled=true.
    // Expect persisted RefundEnabled and AllowUserRefund to both be false.
}
```

Also assert that the factory accepts `payment.TypeXunhuPay`, and that `SupportedTypes()` is exactly `[]string{"wxpay"}`.

**Step 2: Run tests to verify RED**

Run:

```bash
cd backend
go test ./internal/payment/provider ./internal/service -run 'XunhuPay' -count=1
```

Expected: FAIL because `TypeXunhuPay` and `NewXunhuPay` do not exist and the provider key is rejected.

**Step 3: Add the minimal provider contract**

Add:

```go
const TypeXunhuPay PaymentType = "xunhupay"
```

Register `NewXunhuPay` in the factory, add the provider key to `validProviderKeys`, mark `appSecret` sensitive, protect `appId` and `appSecret` while pending orders exist, and normalize create/update requests so XunhuPay cannot persist refund flags.

**Step 4: Run tests to verify GREEN**

Run the same target tests. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/payment/types.go backend/internal/payment/provider/factory.go backend/internal/payment/provider/xunhupay.go backend/internal/payment/provider/xunhupay_test.go backend/internal/service/payment_config_providers.go backend/internal/service/payment_config_providers_test.go
git commit -m "feat(omni): register XunhuPay payment provider"
```

### Task 2: Implement signing and signed HTTP transport

**Files:**
- Create: `backend/internal/payment/provider/xunhupay.go`
- Test: `backend/internal/payment/provider/xunhupay_test.go`

**Step 1: Write failing signing tests**

Cover sorting, empty-value exclusion, `hash` exclusion, extension fields, input-map immutability, and verification rejection:

```go
func TestXunhuPaySignSortsAndExcludesEmptyAndHash(t *testing.T) {
    params := map[string]string{
        "z": "last", "a": "first", "empty": "", "hash": "ignored",
    }
    got := xunhuPaySign(params, "secret")
    require.Equal(t, md5Hex("a=first&z=lastsecret"), got)
    require.Equal(t, "ignored", params["hash"])
}
```

**Step 2: Run the focused test and confirm RED**

```bash
cd backend
go test ./internal/payment/provider -run 'XunhuPaySign' -count=1
```

Expected: FAIL because signing helpers are missing.

**Step 3: Implement minimal signing helpers**

Implement ASCII key sorting, empty/`hash` omission, MD5 lower-case hex output, and constant-time comparison through `crypto/subtle` or `hmac.Equal` over decoded/normalized values. Never mutate caller-owned maps.

**Step 4: Write failing HTTP safety tests**

Use `httptest.Server` to assert context cancellation, timeout-capable client use, non-2xx rejection, a 1 MiB response limit, invalid JSON rejection, upstream error-code propagation, and response-hash rejection.

**Step 5: Run and confirm RED**

```bash
cd backend
go test ./internal/payment/provider -run 'XunhuPayHTTP|XunhuPayResponse' -count=1
```

**Step 6: Implement signed POST transport**

Use form POST to match the official Go example and callback protocol. Add `appid`, Unix `time`, and a cryptographically random `nonce_str`; sign the final parameter set. Resolve endpoints relative to a normalized `apiBase`, set a 10-second client timeout, check 2xx, cap the body, decode JSON, and verify returned `hash` before using data.

**Step 7: Run the provider tests and commit**

```bash
cd backend
go test ./internal/payment/provider -run 'XunhuPay' -count=1
cd ..
git add backend/internal/payment/provider/xunhupay.go backend/internal/payment/provider/xunhupay_test.go
git commit -m "feat(omni): add safe XunhuPay transport and signing"
```

### Task 3: Create, query, and verify XunhuPay payments

**Files:**
- Modify: `backend/internal/payment/provider/xunhupay.go`
- Modify: `backend/internal/payment/provider/xunhupay_test.go`

**Step 1: Write failing create-payment tests**

Assert the request contains `version=1.1`, `trade_order_id`, `total_fee`, `title`, `notify_url`, `return_url`, `plugins=sub2api`, and a valid signature. Assert `url_qrcode` maps to `QRCode` and mobile `url` maps to `PayURL`.

**Step 2: Run and confirm RED**

```bash
cd backend
go test ./internal/payment/provider -run 'XunhuPayCreatePayment' -count=1
```

**Step 3: Implement create-payment mapping**

POST to `/payment/do.html`; require a usable QR code for desktop requests and return `req.OrderID` as the unified `TradeNo`, because XunhuPay's query endpoint accepts the merchant order ID rather than the create response's historical `openid` field.

**Step 4: Write failing query tests**

Assert `/payment/query.html` receives `out_trade_order=<local-order-id>` and maps `OD` to paid, `WP` to pending, and `CD` to failed/cancelled semantics supported by `QueryOrderResponse`.

**Step 5: Implement query mapping and verify GREEN**

```bash
cd backend
go test ./internal/payment/provider -run 'XunhuPayCreatePayment|XunhuPayQueryOrder' -count=1
```

**Step 6: Write failing notification tests**

Cover:

- valid signed `OD` notification;
- bad signature;
- wrong `appid`;
- missing order ID or amount;
- `WP`, `CD`, `RD`, and `UD` not being treated as paid;
- unknown extension fields remaining part of signature verification;
- returned `OrderID=trade_order_id`, `TradeNo=transaction_id`, and parsed amount.

**Step 7: Implement notification verification**

Parse `application/x-www-form-urlencoded`, verify all received non-empty fields except `hash`, verify APPID, and map only `OD` to `NotificationStatusSuccess`. Return non-paid states as non-success notifications so the service never credits them.

**Step 8: Implement explicit unsupported refund**

Return an explicit provider error without contacting upstream. No refund API is added in this release.

**Step 9: Run and commit**

```bash
cd backend
go test ./internal/payment/provider -run 'XunhuPay' -count=1
cd ..
git add backend/internal/payment/provider/xunhupay.go backend/internal/payment/provider/xunhupay_test.go
git commit -m "feat(omni): implement XunhuPay payment lifecycle"
```

### Task 4: Route XunhuPay callbacks through the shared fulfillment path

**Files:**
- Modify: `backend/internal/handler/payment_webhook_handler.go`
- Modify: `backend/internal/server/routes/payment.go`
- Test: `backend/internal/handler/payment_webhook_handler_test.go`
- Test: `backend/internal/service/payment_fulfillment_test.go`

**Step 1: Write failing handler tests**

Assert `/api/v1/payment/webhook/xunhupay` delegates with provider key `xunhupay`, extracts `trade_order_id` before provider resolution, and responds with exact body `success` after a valid notification.

**Step 2: Run and confirm RED**

```bash
cd backend
go test ./internal/handler ./internal/server -run 'XunhuPay' -count=1
```

**Step 3: Implement the route and order-ID extraction**

Add `XunhuPayNotify`, register the POST route, and extend `extractOutTradeNo` to read `trade_order_id` for `xunhupay` without changing EasyPay handling.

**Step 4: Add service-level safety tests**

Using the existing payment fulfillment fixtures, prove that XunhuPay amount mismatch, provider mismatch, and duplicate success notifications cannot credit twice.

**Step 5: Run and commit**

```bash
cd backend
go test ./internal/handler ./internal/server ./internal/service -run 'XunhuPay|PaymentNotification' -count=1
cd ..
git add backend/internal/handler/payment_webhook_handler.go backend/internal/handler/payment_webhook_handler_test.go backend/internal/server/routes/payment.go backend/internal/service/payment_fulfillment_test.go
git commit -m "feat(omni): route XunhuPay payment notifications"
```

### Task 5: Add backend visible-method source routing

**Files:**
- Modify: `backend/internal/service/payment_resume_service.go`
- Modify: `backend/internal/service/payment_visible_method_instances.go`
- Modify: `backend/internal/service/payment_config_service.go`
- Test: `backend/internal/service/payment_resume_service_test.go`
- Test: `backend/internal/service/payment_config_service_test.go`
- Test: `backend/internal/service/payment_config_limits_test.go`
- Test: `backend/internal/service/payment_order_jsapi_test.go`

**Step 1: Write failing source-mapping tests**

Add `VisibleMethodSourceXunhuPayWechat = "xunhupay_wxpay"` and first assert the intended behavior in tests:

```go
require.Equal(t,
    payment.TypeXunhuPay,
    mustProviderKey(VisibleMethodProviderKeyForSource(payment.TypeWxpay, VisibleMethodSourceXunhuPayWechat)),
)
```

Assert source availability becomes true only when an enabled XunhuPay instance supports `wxpay`, and that limits/order creation select that instance rather than official WxPay or EasyPay.

**Step 2: Run and confirm RED**

```bash
cd backend
go test ./internal/service -run 'VisibleMethodSource|XunhuPay' -count=1
```

**Step 3: Implement source normalization and routing**

Extend normalization, source-to-provider mapping, availability construction, visible-provider matching, limit selection, and any resume-token/provider snapshot checks to recognize `xunhupay_wxpay` as a WeChat source backed by provider key `xunhupay`.

**Step 4: Run and commit**

```bash
cd backend
go test ./internal/service -run 'VisibleMethodSource|VisibleMethod|XunhuPay|PaymentOrder' -count=1
cd ..
git add backend/internal/service/payment_resume_service.go backend/internal/service/payment_visible_method_instances.go backend/internal/service/payment_config_service.go backend/internal/service/*_test.go
git commit -m "feat(omni): route WeChat payments through XunhuPay"
```

Before committing, replace the broad test glob in `git add` with the exact modified test files reported by `git diff --name-only`.

### Task 6: Add admin configuration and Chinese/English labels

**Files:**
- Modify: `frontend/src/types/payment.ts`
- Modify: `frontend/src/components/payment/providerConfig.ts`
- Modify: `frontend/src/components/payment/PaymentProviderDialog.vue`
- Modify: `frontend/src/api/admin/settings.ts`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/en/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Test: `frontend/src/components/payment/__tests__/providerConfig.spec.ts`
- Test: `frontend/src/components/payment/__tests__/PaymentProviderDialog.spec.ts`
- Test: `frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts`
- Test: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

**Step 1: Write failing frontend tests**

Assert:

- `xunhupay` supports only `wxpay`;
- callback path is `/api/v1/payment/webhook/xunhupay`;
- fields are `appId`, sensitive `appSecret`, and defaulted `apiBase`;
- `xunhupay_wxpay` appears only in WeChat source options and normalizes correctly;
- selecting XunhuPay creates a provider with refund flags false;
- the dialog does not render refund controls for XunhuPay.

**Step 2: Run and confirm RED**

```bash
pnpm --dir frontend vitest run \
  src/components/payment/__tests__/providerConfig.spec.ts \
  src/components/payment/__tests__/PaymentProviderDialog.spec.ts \
  src/api/__tests__/settings.paymentVisibleMethods.spec.ts \
  src/views/admin/__tests__/SettingsView.spec.ts
```

Expected: FAIL because XunhuPay is not in the frontend contract.

**Step 3: Implement frontend contract**

Add provider option and translations, config fields, callback composition, `xunhupay_wxpay` source option/alias, and the provider visibility method mapping. Hide refund toggles when `provider_key === "xunhupay"` and force submitted refund flags to false.

**Step 4: Run and commit**

Run the same Vitest command. Expected: PASS.

```bash
git add frontend/src/types/payment.ts frontend/src/components/payment/providerConfig.ts frontend/src/components/payment/PaymentProviderDialog.vue frontend/src/api/admin/settings.ts frontend/src/views/admin/SettingsView.vue frontend/src/i18n/locales/zh/admin/settings.ts frontend/src/i18n/locales/en/admin/settings.ts frontend/src/i18n/locales/zh/misc.ts frontend/src/i18n/locales/en/misc.ts frontend/src/components/payment/__tests__/providerConfig.spec.ts frontend/src/components/payment/__tests__/PaymentProviderDialog.spec.ts frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts frontend/src/views/admin/__tests__/SettingsView.spec.ts
git commit -m "feat(omni): configure XunhuPay in payment settings"
```

### Task 7: Regression verification and branch-scope audit

**Files:**
- Modify only if tests expose a XunhuPay regression in files already named above.

**Step 1: Format changed source files**

```bash
gofmt -w backend/internal/payment/provider/xunhupay.go backend/internal/payment/provider/xunhupay_test.go backend/internal/payment/types.go backend/internal/payment/provider/factory.go backend/internal/handler/payment_webhook_handler.go backend/internal/server/routes/payment.go backend/internal/service/payment_config_providers.go backend/internal/service/payment_resume_service.go backend/internal/service/payment_visible_method_instances.go backend/internal/service/payment_config_service.go
pnpm --dir frontend exec prettier --write <exact changed frontend files>
```

**Step 2: Run backend payment regression suite**

```bash
cd backend
go test ./internal/payment/... ./internal/handler ./internal/service ./internal/server -count=1
go test ./... -count=1
```

Expected: PASS. If the repository has a pre-existing unrelated failure, capture the exact failure and prove all changed-package tests pass.

**Step 3: Run frontend regression checks**

```bash
pnpm --dir frontend test -- --run
pnpm --dir frontend run type-check
```

Expected: PASS, subject to documented repository baseline failures.

**Step 4: Run build/static checks**

```bash
cd backend
go vet ./internal/payment/... ./internal/handler ./internal/service ./internal/server
go build ./cmd/server
```

**Step 5: Audit scope**

```bash
git branch --show-current
git status --short --branch
git diff --name-only origin/dev/omni...HEAD
git log --oneline origin/dev/omni..HEAD
```

Expected: branch is `dev/omni`; no company/team/main changes exist; pre-existing `deploy/omni-v2/` and `docs/plans/2026-07-21-omni-v2-dual-service-deployment.md` remain untouched and uncommitted.

**Step 6: Record final verification commit only if needed**

If formatting or test-driven fixes changed tracked files after the last feature commit, stage only those exact files and commit:

```bash
git commit -m "test(omni): verify XunhuPay payment integration"
```

Do not deploy, push, create a live provider instance, or call real XunhuPay endpoints in this plan. Those require the merchant APPID/APPSECRET plus explicit deployment and small-value payment authorization.
