# XunhuPay Provider Run Log

Date: 2026-07-22
Branch: `dev/omni`

## Delivered scope

- Registered `xunhupay` as a first-class payment provider.
- Added signed create, query, and notification handling for WeChat QR-code balance recharge.
- Added `/api/v1/payment/webhook/xunhupay` and routed successful notifications through the existing idempotent fulfillment path.
- Added explicit `xunhupay_wxpay` visible-method routing.
- Added admin configuration for `APPID`, `APPSECRET`, API base URL, and fixed callback URLs.
- Forced refunds off in both backend normalization and the admin UI.

## Verification evidence

- `go test ./internal/payment/... ./internal/handler ./internal/server/routes -count=1` — passed.
- `go test ./internal/service -run 'Test(XunhuPay|NormalizeProviderRefundFlagsDisablesXunhuPayRefunds|PaymentConfigProvider)' -count=1` — passed.
- `go vet ./internal/payment/... ./internal/handler ./internal/server/routes` — passed.
- `go build ./cmd/server` — passed.
- `pnpm test:run` — passed, 141 files and 917 tests.
- `pnpm build` — passed.
- ESLint over all touched frontend files — passed.
- `git diff --check` — passed.

## Remaining boundary

- No live provider instance was created because merchant credentials were not supplied.
- No deployment or real-money transaction was performed.
- Refunds are intentionally unsupported in the first release.
- One full `go test ./... -count=1` run ended in the existing `internal/service` suite after the XunhuPay-focused service tests had passed; this repository suite includes environment-dependent integration behavior and is not used as proof for the provider slice.
