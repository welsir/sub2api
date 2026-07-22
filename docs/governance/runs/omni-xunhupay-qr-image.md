# Omni Xunhupay QR Image Run

## Boundary

- Branch: `dev/omni`
- Payment method: WeChat through Xunhupay only
- Excluded: Alipay, Stripe, deployment, and unrelated existing working-tree changes

## Evidence

- RED: provider test failed because `CreatePaymentResponse` had no QR-image field.
- RED: frontend launch test returned `redirect_waiting` instead of `qr_image_waiting`.
- RED: status panel did not request or render the proxied image and did not retry a transient failure.
- RED: desktop create response leaked the Xunhupay mobile URL; mobile response selected the wrong action.
- GREEN: targeted provider, service, route, flow, and status-panel tests passed.
- `go test ./... -count=1` passed for the backend workspace.
- `pnpm exec vitest run` passed: 141 files and 920 tests.
- `pnpm run typecheck` passed.
- `pnpm run build` passed; Vite emitted only its existing large-chunk warning.

## Result

- Desktop Xunhupay checkout returns `QR_IMAGE` with `/payment/orders/{id}/qr-image` and no provider URL.
- Mobile Xunhupay checkout retains `REDIRECT` behavior.
- The authenticated image endpoint validates ownership, pending/expiry state, provider identity, host, redirects, PNG type/signature, and a 512 KiB size limit.
- Frontend fetches the image with the authenticated API client, renders it directly, retries transient failures, and revokes object URLs.

## Remaining Risk

- Production deployment and a real ¥0.01 payment were not part of this local-only change.
- The separately observed Xunhupay query response missing `hash` remains outside this QR rendering fix; webhook settlement behavior is unchanged.
