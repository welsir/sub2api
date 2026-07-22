# Omni Xunhupay QR Image Fix

## Scope

Fix WeChat recharge on `dev/omni` when Xunhupay returns `url_qrcode`, which is an already-rendered QR image URL rather than QR content.

## Requirements

- Treat Xunhupay `url_qrcode` as a QR image, never as text to encode again.
- Keep raw provider image URLs private; the browser may only request an authenticated Sub2API endpoint.
- Preserve existing raw QR-code behavior for native `weixin://` and other `QR_CODE` providers.
- Validate the upstream image URL, redirects, response type, and response size before proxying it.
- Do not change Stripe or Alipay behavior.

## Acceptance

- Xunhupay create-payment responses select `QR_IMAGE` and expose only a Sub2API image endpoint.
- The payment UI renders the fetched image directly and does not call the QR encoder for it.
- Existing `QR_CODE` tests continue to pass.
- Provider image fetches reject untrusted hosts and non-PNG/oversized responses.
