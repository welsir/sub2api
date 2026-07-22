# Omni Xunhupay QR Image Run

## Boundary

- Branch: `dev/omni`
- Payment method: WeChat through Xunhupay only
- Production target: host 43 V2 (`sub2api-v2-app`) only
- Excluded: Alipay, Stripe, Legacy, and unrelated existing working-tree changes

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
- Built `tml/sub2api:v0.1.151-omni-wechat-qr-20260722` for `linux/amd64`; the image reports commit `25030ca0`.
- The uploaded archive SHA-256 matched locally and remotely: `2a0e54a91b314a22b3ababa52aa596e132969453e4b5fed6770ba246027781e5`.
- Backed up the production compose file as `docker-compose.yml.pre-wechat-qr-20260722T1622CST` before changing the V2 app image.
- After replacement, V2 local and public health checks returned `{"status":"ok"}` and the container reported `healthy` with zero restarts.
- The public payment bundle contains the authenticated `qr-image` fetch and `createObjectURL` direct-image path.
- The new QR-image route returned HTTP 401 without a session, confirming that the route is deployed and remains authenticated.
- PostgreSQL, Redis, and Legacy retained their prior start times; Legacy remained healthy on its previous image.

## Result

- Desktop Xunhupay checkout returns `QR_IMAGE` with `/payment/orders/{id}/qr-image` and no provider URL.
- Mobile Xunhupay checkout retains `REDIRECT` behavior.
- The authenticated image endpoint validates ownership, pending/expiry state, provider identity, host, redirects, PNG type/signature, and a 512 KiB size limit.
- Frontend fetches the image with the authenticated API client, renders it directly, retries transient failures, and revokes object URLs.
- Production V2 now runs `tml/sub2api:v0.1.151-omni-wechat-qr-20260722`.

## Remaining Risk

- A new authenticated WeChat order and real scan/payment have not yet been executed after deployment.
- The separately observed Xunhupay query response missing `hash` remains outside this QR rendering fix; webhook settlement behavior is unchanged.
